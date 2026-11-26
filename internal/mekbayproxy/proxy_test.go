package mekbayproxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/davidzech/Battletech/internal/storage"
)

func TestMekBayHostParsing(t *testing.T) {
	id := storage.NewLinkID()
	for _, c := range []struct {
		host string
		id   string
		kind mekbayHostKind
	}{
		{"mekbay-" + id + ".battletech.zech.io", id, linkHost},
		{"mekbay-" + id + ".localhost:8080", id, linkHost},
		{"MekBay-" + strings.ToUpper(id) + ".Example.com", id, linkHost},
		{"mekbay-short.battletech.zech.io", "", linkHost},
		{"mekbay-" + id + "x.battletech.zech.io", "", linkHost},
		{"mekbay-" + strings.ReplaceAll(id, id[:1], "1") + ".x.io", "", linkHost}, // 1 is not a base32 character here
		{"mekbay.localhost:8080", "", sandboxHost},
		{"mekbay.battletech.zech.io", "", sandboxHost},
		{"battletech.zech.io", "", notMekBayHost},
		{"localhost:8080", "", notMekBayHost},
		{"notmekbay.example.com", "", notMekBayHost},
		{"x.mekbay.example.com", "", notMekBayHost},
		{"mekbayfoo.example.com", "", notMekBayHost},
	} {
		if gid, kind := parseMekBayHost(c.host); gid != c.id || kind != c.kind {
			t.Errorf("parseMekBayHost(%q) = %q, %v; want %q, %v", c.host, gid, kind, c.id, c.kind)
		}
	}
	if !IsHost("mekbay.localhost") || IsHost("battletech.zech.io") {
		t.Error("isMekBayHost follows the kind")
	}
	for host, want := range map[string]string{
		"mekbay-" + id + ".battletech.zech.io": "battletech.zech.io",
		"mekbay.localhost:8080":                "localhost:8080",
		"battletech.zech.io":                   "battletech.zech.io",
		"localhost:8080":                       "localhost:8080",
	} {
		if got := SiteHost(host); got != want {
			t.Errorf("siteHost(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestSwapUUID(t *testing.T) {
	const acct = testAccount
	for in, want := range map[string]string{
		`{"action":"x"}`:                      `{"action":"x"}`,
		`{"uuid":"a"}`:                        `{"uuid":"` + acct + `"}`,
		`{"a":1,"uuid":"a","b":{"uuid":"n"}}`: `{"a":1,"b":{"uuid":"n"},"uuid":"` + acct + `"}`,
		`[{"uuid":"a"}]`:                      `[{"uuid":"a"}]`,
		`{"note":"has \"uuid\" in text"}`:     `{"note":"has \"uuid\" in text"}`,
		`{"uuid":"a","data":{"n":1.50}}`:      `{"data":{"n":1.50},"uuid":"` + acct + `"}`,
	} {
		if got := string(swapUUID([]byte(in), acct)); got != want {
			t.Errorf("swapUUID(%s)\n got %s\nwant %s", in, got, want)
		}
	}
}

// cacheUpstream is a stand-in for mekbay.com's files: every path under /files/ is a file holding its own path, /gone/ is a 404, anything
// else is the page. While down it answers everything with a 503.
type cacheUpstream struct {
	*httptest.Server
	mu   sync.Mutex
	hits map[string]int
	down bool
}

func newCacheUpstream(t *testing.T) (*cacheUpstream, *Proxy) {
	t.Helper()
	u := &cacheUpstream{hits: map[string]int{}}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		u.hits[r.URL.Path]++
		down := u.down
		u.mu.Unlock()
		switch {
		case down:
			http.Error(w, "upstream is unwell", http.StatusServiceUnavailable)
		case strings.HasPrefix(r.URL.Path, "/gone/"):
			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.Path, "/files/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, r.URL.Path)
		default:
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<html>page</html>")
		}
	}))
	t.Cleanup(u.Close)
	p := New(nil)
	p.Web = u.URL
	return u, p
}

func (u *cacheUpstream) count(path string) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.hits[path]
}

func (u *cacheUpstream) setDown(down bool) {
	u.mu.Lock()
	u.down = down
	u.mu.Unlock()
}

func fetch(t *testing.T, p *Proxy, path string) *proxiedFile {
	t.Helper()
	f, err := p.file(context.Background(), path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return f
}

// checkCache fails the test unless the cache's bookkeeping agrees with what it holds and it is within its limits.
func checkCache(t *testing.T, p *Proxy) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	var held int
	for e := p.lru.Front(); e != nil; e = e.Next() {
		f := e.Value.(*proxiedFile)
		held += f.size()
		if p.cache[f.path] != e {
			t.Errorf("%s is in the list but not the map", f.path)
		}
	}
	if len(p.cache) != p.lru.Len() || held != p.cacheBytes {
		t.Errorf("cache bookkeeping: %d in the map, %d in the list, %d bytes counted, %d held", len(p.cache), p.lru.Len(), p.cacheBytes, held)
	}
	if len(p.cache) > p.maxEntries || p.cacheBytes > p.maxBytes {
		t.Errorf("the cache is over its limits: %d entries, %d bytes", len(p.cache), p.cacheBytes)
	}
}

// A full cache still takes new files: the ones used longest ago make room, so junk paths can't lock real files out.
func TestFileCacheEvictsLeastRecentlyUsed(t *testing.T) {
	u, p := newCacheUpstream(t)
	p.maxEntries = 3
	for _, path := range []string{"/files/a", "/files/b", "/files/c"} {
		fetch(t, p, path)
	}
	fetch(t, p, "/files/a") // a is now the most recently used, b the least
	for _, junk := range []string{"/junk-1", "/gone/2"} {
		fetch(t, p, junk)
	}
	checkCache(t, p)
	if fetch(t, p, "/files/a"); u.count("/files/a") != 1 {
		t.Errorf("the file used most recently is still cached: fetched %d times", u.count("/files/a"))
	}
	// A new file is cached although the cache was full...
	fetch(t, p, "/files/new")
	fetch(t, p, "/files/new")
	if n := u.count("/files/new"); n != 1 {
		t.Errorf("a full cache still takes a new file: fetched %d times", n)
	}
	// ...and the ones pushed out are simply fetched again.
	if f := fetch(t, p, "/files/b"); u.count("/files/b") != 2 || string(f.body) != "/files/b" {
		t.Errorf("an evicted file is fetched again: %d times, %q", u.count("/files/b"), f.body)
	}
	checkCache(t, p)
}

func TestFileCacheIsBoundedByBytes(t *testing.T) {
	u, p := newCacheUpstream(t)
	one := (&proxiedFile{path: "/files/a", contentType: "application/json", body: []byte("/files/a")}).size()
	p.maxBytes = 2*one + one/2 // room for two such files
	for _, path := range []string{"/files/a", "/files/b", "/files/c"} {
		fetch(t, p, path)
	}
	checkCache(t, p)
	if len(p.cache) != 2 || p.cache["/files/a"] != nil {
		t.Errorf("the oldest file made room: %d cached", len(p.cache))
	}
	// A file bigger than the whole cache is served but not kept, and pushes nothing out.
	big := "/files/" + strings.Repeat("x", 3*one)
	if f := fetch(t, p, big); string(f.body) != big {
		t.Errorf("a file too big to cache is still served")
	}
	fetch(t, p, big)
	if u.count(big) != 2 || len(p.cache) != 2 {
		t.Errorf("a file too big to cache is not kept: fetched %d times, %d cached", u.count(big), len(p.cache))
	}
	checkCache(t, p)
	// Replacing a copy counts its bytes once.
	p.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	fetch(t, p, "/files/c")
	if u.count("/files/c") != 2 {
		t.Errorf("a stale file is fetched again: %d", u.count("/files/c"))
	}
	checkCache(t, p)
}

// A path MekBay doesn't have is not held on to for as long as a file.
func TestFileCacheForgetsMissingPathsSoon(t *testing.T) {
	u, p := newCacheUpstream(t)
	now := time.Now()
	p.Now = func() time.Time { return now }
	if f := fetch(t, p, "/gone/x.js"); f.status != http.StatusNotFound {
		t.Fatalf("status %d", f.status)
	}
	fetch(t, p, "/files/a")
	now = now.Add(30 * time.Second)
	fetch(t, p, "/gone/x.js")
	if n := u.count("/gone/x.js"); n != 1 {
		t.Errorf("a 404 is remembered for a moment: fetched %d times", n)
	}
	now = now.Add(2 * time.Minute)
	fetch(t, p, "/gone/x.js")
	fetch(t, p, "/files/a")
	if u.count("/gone/x.js") != 2 || u.count("/files/a") != 1 {
		t.Errorf("a 404 is asked for again after a minute, a file is not: %d, %d", u.count("/gone/x.js"), u.count("/files/a"))
	}
}

// When MekBay answers with a server error, the last good copy is served instead; without one the error passes through and is not cached.
func TestFileServesStaleCopyOnUpstreamError(t *testing.T) {
	u, p := newCacheUpstream(t)
	now := time.Now()
	p.Now = func() time.Time { return now }
	fetch(t, p, "/files/a")
	now = now.Add(3 * time.Hour)
	u.setDown(true)
	if f := fetch(t, p, "/files/a"); f.status != http.StatusOK || string(f.body) != "/files/a" {
		t.Errorf("the last copy is served while MekBay answers 503: %d %q", f.status, f.body)
	}
	if u.count("/files/a") != 2 {
		t.Errorf("MekBay was asked first: %d", u.count("/files/a"))
	}
	if f := fetch(t, p, "/files/never"); f.status != http.StatusServiceUnavailable {
		t.Errorf("with no copy the error passes through: %d", f.status)
	}
	checkCache(t, p)
	if len(p.cache) != 1 {
		t.Errorf("a server error is not cached: %d", len(p.cache))
	}
	// Once MekBay is back the fresh file replaces the stale one.
	u.setDown(false)
	fetch(t, p, "/files/a")
	fetch(t, p, "/files/a")
	if u.count("/files/a") != 3 {
		t.Errorf("the recovered file is cached again: %d", u.count("/files/a"))
	}
}
