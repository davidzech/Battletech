package site

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const (
	testAccount  = "0f1e2d3c-4b5a-4968-8776-655443322110"
	otherAccount = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
)

// fakeUpstream is a stand-in for mekbay.com: a static page and script naming the WebSocket the way the real build does, a manifest, and a
// WebSocket that records what it is sent.
type fakeUpstream struct {
	*httptest.Server
	mu       sync.Mutex
	received []string
	origins  []string
	hits     map[string]int
	scripts  string // contents of /chunk-abc.js
}

func newFakeUpstream(t *testing.T) *fakeUpstream {
	t.Helper()
	f := &fakeUpstream{hits: map[string]int{}, scripts: "var M=class{ws=null;wsUrl=`wss://mekbay.com/ws`;x=1};"}
	mux := http.NewServeMux()
	count := func(path string) {
		f.mu.Lock()
		f.hits[path]++
		f.mu.Unlock()
	}
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer c.CloseNow()
		f.mu.Lock()
		f.origins = append(f.origins, r.Header.Get("Origin"))
		f.mu.Unlock()
		for {
			typ, b, err := c.Read(r.Context())
			if err != nil {
				return
			}
			f.mu.Lock()
			f.received = append(f.received, string(b))
			f.mu.Unlock()
			if err := c.Write(r.Context(), typ, append([]byte("echo:"), b...)); err != nil {
				return
			}
		}
	})
	mux.HandleFunc("/ngsw.json", func(w http.ResponseWriter, r *http.Request) {
		count(r.URL.Path)
		_, _ = io.WriteString(w, `{"assetGroups":[{"name":"app","urls":["/index.html","/main-1.js","/chunk-abc.js","/styles.css"]}]}`)
	})
	mux.HandleFunc("/main-1.js", func(w http.ResponseWriter, r *http.Request) {
		count(r.URL.Path)
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = io.WriteString(w, "console.log('main');")
	})
	mux.HandleFunc("/chunk-abc.js", func(w http.ResponseWriter, r *http.Request) {
		count(r.URL.Path)
		w.Header().Set("Content-Type", "text/javascript")
		f.mu.Lock()
		s := f.scripts
		f.mu.Unlock()
		_, _ = io.WriteString(w, s)
	})
	mux.HandleFunc("/ngsw-worker.js", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "REAL WORKER") })
	mux.HandleFunc("/assets/data.json", func(w http.ResponseWriter, r *http.Request) {
		count(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { // the SPA page, also the fallback for every other path
		count("/")
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<html><base href="/"><script src="main-1.js"></script></html>`)
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeUpstream) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.received...)
}

// proxyFixture is the site with its MekBay proxy pointed at a fake MekBay, and a client that reaches any host name as this machine.
type proxyFixture struct {
	fake   *fakeUpstream
	site   *httptest.Server
	proxy  *mekbayProxy
	store  *Store
	client *http.Client
	port   string
}

func newProxyFixture(t *testing.T) *proxyFixture {
	t.Helper()
	fake := newFakeUpstream(t)
	store, err := OpenSQLite(t.TempDir() + "/proxy.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if _, err := store.EnsurePlaythrough(context.Background(), "pt"); err != nil {
		t.Fatal(err)
	}
	var proxy *mekbayProxy
	site := httptest.NewServer(NewHandler(t.TempDir(), store,
		withMekBayUpstream(fake.URL, "ws"+strings.TrimPrefix(fake.URL, "http")+"/ws", "https://mekbay.com"),
		withMekBayProxyHook(func(p *mekbayProxy) { proxy = p })))
	t.Cleanup(site.Close)
	addr := strings.TrimPrefix(site.URL, "http://")
	_, port, _ := net.SplitHostPort(addr)
	client := &http.Client{
		Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		}},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &proxyFixture{fake: fake, site: site, proxy: proxy, store: store, client: client, port: port}
}

// link makes (or finds) the link for account under a label and returns its host name, such as mekbay-<id>.localhost.
func (p *proxyFixture) link(t *testing.T, label, account string) string {
	t.Helper()
	id, err := p.store.EnsureMekBayLink(context.Background(), "pt", "camp", label, account, time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return mekbayLinkPrefix + id + ".localhost"
}

func (p *proxyFixture) get(t *testing.T, host, path string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", "http://"+host+":"+p.port+path, nil)
	resp, err := p.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func (p *proxyFixture) dial(t *testing.T, host string, extra http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	header := http.Header{"Origin": {"http://" + host + ":" + p.port}}
	for k, v := range extra {
		header[k] = v
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return websocket.Dial(ctx, "ws://"+host+":"+p.port+"/ws", &websocket.DialOptions{HTTPClient: p.client, HTTPHeader: header})
}

// who sends a register message carrying a made-up identity and reports the account that reached MekBay.
func whoIs(t *testing.T, conn *websocket.Conn) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"action":"register","uuid":"11111111-1111-4111-8111-111111111111"}`)); err != nil {
		t.Fatal(err)
	}
	_, b, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var m struct{ UUID string }
	_ = json.Unmarshal([]byte(strings.TrimPrefix(string(b), "echo:")), &m)
	return m.UUID
}

func TestMekBayHostParsing(t *testing.T) {
	id := newMekBayLinkID()
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
	if !isMekBayHost("mekbay.localhost") || isMekBayHost("battletech.zech.io") {
		t.Error("isMekBayHost follows the kind")
	}
	for host, want := range map[string]string{
		"mekbay-" + id + ".battletech.zech.io": "battletech.zech.io",
		"mekbay.localhost:8080":                "localhost:8080",
		"battletech.zech.io":                   "battletech.zech.io",
		"localhost:8080":                       "localhost:8080",
	} {
		if got := siteHost(host); got != want {
			t.Errorf("siteHost(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestMekBayProxyServesAndRewrites(t *testing.T) {
	p := newProxyFixture(t)
	host := p.link(t, "player-2", testAccount)

	resp, body := p.get(t, host, "/")
	if resp.StatusCode != 200 || !strings.Contains(body, `<base href="/">`) || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("a valid link serves MekBay's own page: %d %q", resp.StatusCode, body)
	}
	// An unknown route falls back to the page, as MekBay does.
	if _, body := p.get(t, host, "/some/route"); !strings.Contains(body, "main-1.js") {
		t.Errorf("an unknown path serves the page: %q", body)
	}

	// Every response keeps the host name to itself.
	for _, path := range []string{"/", "/chunk-abc.js", "/assets/data.json", "/ngsw-worker.js"} {
		r, _ := p.get(t, host, path)
		if r.Header.Get("Referrer-Policy") != "no-referrer" || r.Header.Get("X-Robots-Tag") != "noindex" || r.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: the headers keep the host name from leaking: %v", path, r.Header)
		}
	}
	if r, _ := p.get(t, host, "/"); r.Header.Get("Cache-Control") != "no-store" {
		t.Error("the page is not cached")
	}

	// The script that names the WebSocket now names this host's relay; the rest of it is untouched.
	_, js := p.get(t, host, "/chunk-abc.js")
	if want := "var M=class{ws=null;wsUrl=`ws://" + host + ":" + p.port + "/ws`;x=1};"; js != want {
		t.Errorf("the WebSocket address is replaced:\n got %q\nwant %q", js, want)
	}
	if strings.Contains(js, "mekbay.com") {
		t.Error("nothing still names mekbay.com")
	}
	// ...and a different link's host gets its own address.
	other := p.link(t, "opfor", otherAccount)
	if _, js := p.get(t, other, "/chunk-abc.js"); !strings.Contains(js, "ws://"+other+":"+p.port+"/ws") || strings.Contains(js, host) {
		t.Errorf("each host's script names that host: %q", js)
	}
	if _, js := p.get(t, host, "/main-1.js"); js != "console.log('main');" {
		t.Errorf("a script without the address is untouched: %q", js)
	}
	// Behind TLS (X-Forwarded-Proto) the address is wss.
	req, _ := http.NewRequest("GET", "http://"+host+":"+p.port+"/chunk-abc.js", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	resp2, err := p.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if !strings.Contains(string(b), "`wss://"+host) {
		t.Errorf("behind https the address is wss: %q", b)
	}
	if _, body := p.get(t, host, "/assets/data.json"); body != `{"ok":true}` {
		t.Errorf("data passes through: %q", body)
	}

	// The service workers are stubs that unregister themselves, and the manifest that would feed them is gone.
	for _, path := range []string{"/ngsw-worker.js", "/safety-worker.js"} {
		resp, body := p.get(t, host, path)
		if resp.StatusCode != 200 || !strings.Contains(body, "unregister()") || strings.Contains(body, "REAL WORKER") {
			t.Errorf("%s is the stub: %d %q", path, resp.StatusCode, body)
		}
	}
	if resp, _ := p.get(t, host, "/ngsw.json"); resp.StatusCode != 404 {
		t.Errorf("the service worker manifest is gone: %d", resp.StatusCode)
	}
	// Only reading is allowed.
	post, _ := http.NewRequest("POST", "http://"+host+":"+p.port+"/", strings.NewReader("x"))
	if resp, err := p.client.Do(post); err != nil || resp.StatusCode != 405 {
		t.Errorf("POST is refused: %v %v", resp, err)
	}
}

// A host whose link isn't good gets no pages (the files are MekBay's own public ones, so they are not secret).
func TestMekBayProxyRefusesBadLinks(t *testing.T) {
	p := newProxyFixture(t)
	expired, err := p.store.EnsureMekBayLink(context.Background(), "pt", "camp", "old", testAccount, -time.Minute, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	revoked := p.link(t, "gone", otherAccount)
	if ok, err := p.store.RevokeMekBayLink(context.Background(), "pt", "camp", "gone"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	for name, host := range map[string]string{
		"an unknown id":   mekbayLinkPrefix + newMekBayLinkID() + ".localhost",
		"a malformed id":  "mekbay-nope.localhost",
		"an expired link": mekbayLinkPrefix + expired + ".localhost",
		"a revoked link":  revoked,
	} {
		resp, body := p.get(t, host, "/")
		if resp.StatusCode != http.StatusForbidden || strings.Contains(body, "base href") || !strings.Contains(body, "not valid or has expired") {
			t.Errorf("%s gets no page: %d %q", name, resp.StatusCode, body)
		}
		if resp.Header.Get("Referrer-Policy") != "no-referrer" || resp.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("%s: the refusal keeps its headers: %v", name, resp.Header)
		}
		if _, resp, err := p.dial(t, host, nil); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s gets no relay: %v %v", name, resp, err)
		}
		// Files are public MekBay files, so they are not refused.
		if r, _ := p.get(t, host, "/assets/data.json"); r.StatusCode != 200 {
			t.Errorf("%s: a public file is still served: %d", name, r.StatusCode)
		}
	}
	if n := len(p.fake.sent()); n != 0 {
		t.Errorf("nothing reached MekBay: %d", n)
	}
}

func TestMekBayProxyCachesAndSurvivesAnOutage(t *testing.T) {
	p := newProxyFixture(t)
	host := p.link(t, "player-1", testAccount)
	for i := 0; i < 3; i++ {
		p.get(t, host, "/assets/data.json")
	}
	if n := p.fake.hits["/assets/data.json"]; n != 1 {
		t.Errorf("a fresh file is fetched once, not %d times", n)
	}
	// Let the copy go stale and take MekBay down: the old copy is served instead of an error.
	p.proxy.now = func() time.Time { return time.Now().Add(3 * time.Hour) }
	p.fake.Close()
	resp, body := p.get(t, host, "/assets/data.json")
	if resp.StatusCode != 200 || body != `{"ok":true}` {
		t.Errorf("the last copy is served while MekBay is down: %d %q", resp.StatusCode, body)
	}
	if resp, _ := p.get(t, host, "/never-seen.js"); resp.StatusCode != 502 {
		t.Errorf("a file never seen is a 502 while MekBay is down: %d", resp.StatusCode)
	}
}

func TestMekBayRelaySwapsTheIdentity(t *testing.T) {
	p := newProxyFixture(t)
	host := p.link(t, "player-1", testAccount)
	conn, _, err := p.dial(t, host, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	roundTrip := func(msg string) string {
		t.Helper()
		if err := conn.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
			t.Fatal(err)
		}
		_, b, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	// The browser's own random identity is replaced; everything else, including a nested "uuid", is passed through.
	got := roundTrip(`{"action":"register","uuid":"11111111-1111-4111-8111-111111111111","version":2}`)
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(got, "echo:")), &m); err != nil {
		t.Fatalf("%q: %v", got, err)
	}
	if m["uuid"] != testAccount || m["action"] != "register" || m["version"] != float64(2) {
		t.Errorf("register carries the account: %v", m)
	}
	nested := `{"action":"saveForce","uuid":"11111111-1111-4111-8111-111111111111","data":{"uuid":"keep-me","units":[1,2,3],"big":12345678901234567890}}`
	got = strings.TrimPrefix(roundTrip(nested), "echo:")
	var n struct {
		UUID string          `json:"uuid"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &n); err != nil || n.UUID != testAccount || string(n.Data) != `{"uuid":"keep-me","units":[1,2,3],"big":12345678901234567890}` {
		t.Errorf("nested data is untouched: %q (%v)", got, err)
	}
	// A message with no uuid, or not JSON at all, is passed along exactly.
	if got := roundTrip(`{"action":"ping"}`); got != `echo:{"action":"ping"}` {
		t.Errorf("a message without a uuid is untouched: %q", got)
	}
	if got := roundTrip(`not json "uuid"`); got != `echo:not json "uuid"` {
		t.Errorf("garbage is passed along: %q", got)
	}
	for _, s := range p.fake.sent() {
		if strings.Contains(s, "11111111-1111-4111-8111-111111111111") {
			t.Errorf("the browser's own identity never reaches MekBay: %q", s)
		}
	}
	p.fake.mu.Lock()
	origins := append([]string(nil), p.fake.origins...)
	p.fake.mu.Unlock()
	if len(origins) != 1 || origins[0] != "https://mekbay.com" {
		t.Errorf("the relay presents itself to MekBay as the real site: %v", origins)
	}
}

// Every link is its own host, so one person can hold two accounts at once, and a link keeps working after its connection closes.
func TestMekBayRelayTwoLinksTwoAccounts(t *testing.T) {
	p := newProxyFixture(t)
	player, opfor := p.link(t, "player-1", testAccount), p.link(t, "opfor", otherAccount)
	if player == opfor {
		t.Fatal("different labels are different links")
	}
	a, _, err := p.dial(t, player, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := p.dial(t, opfor, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.CloseNow()
	if got, want := whoIs(t, a), testAccount; got != want {
		t.Errorf("the player link acts as the player: %q", got)
	}
	if got := whoIs(t, b); got != otherAccount {
		t.Errorf("the OpFor link acts as the OpFor while the other is open: %q", got)
	}
	a.CloseNow()
	// "Closing the tab and opening the same address again": a new connection to the same host is the same account.
	again, _, err := p.dial(t, player, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer again.CloseNow()
	if got := whoIs(t, again); got != testAccount {
		t.Errorf("the same address is the same account after reconnecting: %q", got)
	}
	// Revoking a link stops it at once; asking again makes a new host.
	if ok, err := p.store.RevokeMekBayLink(context.Background(), "pt", "camp", "player-1"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, resp, err := p.dial(t, player, nil); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a revoked link no longer connects: %v %v", resp, err)
	}
	if fresh := p.link(t, "player-1", testAccount); fresh == player {
		t.Error("a revoked label gets a new link")
	}
	// A page from another origin can't use a link's host through a visitor's browser.
	_, resp, err := p.dial(t, opfor, http.Header{"Origin": {"https://evil.example"}})
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("a page from another origin is refused: %v %v", resp, err)
	}
}

// On a developer's machine (the bare mekbay.localhost, from loopback), a browser acts as one shared sandbox account; anywhere else the bare
// host gets no relay, so the deployed site is never an open door to MekBay.
func TestMekBayRelaySandbox(t *testing.T) {
	p := newProxyFixture(t)
	a, _, err := p.dial(t, "mekbay.localhost", nil)
	if err != nil {
		t.Fatalf("the bare localhost host is let in: %v", err)
	}
	defer a.CloseNow()
	b, _, err := p.dial(t, "mekbay.localhost", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.CloseNow()
	sandbox := sandboxAccount
	if ua, ub := whoIs(t, a), whoIs(t, b); ua != sandbox || ub != sandbox || sandbox == testAccount || len(sandbox) != 36 {
		t.Errorf("everyone on the bare host shares one account: %q %q want %q", ua, ub, sandbox)
	}
	// A link still wins on its own host, and the sandbox isn't offered on link hosts that don't work.
	c, _, err := p.dial(t, p.link(t, "player-1", testAccount), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if got := whoIs(t, c); got != testAccount {
		t.Errorf("a link acts as its own account: %q", got)
	}
	if _, resp, err := p.dial(t, "mekbay-nope.localhost", nil); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a broken link host gets no sandbox: %v %v", resp, err)
	}

	// Not on localhost, or not from this machine, there is no sandbox: the host name alone proves nothing.
	for name, c := range map[string]struct {
		host  string
		extra http.Header
	}{
		"a deployed host":       {"mekbay.battletech.zech.io", nil},
		"a lookalike":           {"mekbay.notlocalhost", nil},
		"behind a proxy (XFF)":  {"mekbay.localhost", http.Header{"X-Forwarded-For": {"203.0.113.9"}}},
		"behind a proxy (Fwd)":  {"mekbay.localhost", http.Header{"Forwarded": {"for=203.0.113.9"}}},
		"behind a proxy (host)": {"mekbay.localhost", http.Header{"X-Forwarded-Host": {"battletech.zech.io"}}},
	} {
		if _, resp, err := p.dial(t, c.host, c.extra); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s gets no sandbox: %v %v", name, resp, err)
		}
	}
	if sandboxAllowed(&http.Request{Host: "mekbay.localhost:8080", RemoteAddr: "203.0.113.9:5000", Header: http.Header{}}) {
		t.Error("a remote client can't claim localhost with its Host header")
	}
	if !sandboxAllowed(&http.Request{Host: "mekbay.localhost:8080", RemoteAddr: "[::1]:5000", Header: http.Header{}}) || !sandboxAllowed(&http.Request{Host: "mekbay-x.localhost", RemoteAddr: "127.0.0.1:5000", Header: http.Header{}}) {
		t.Error("loopback clients on *.localhost are allowed, IPv6 included")
	}
	if n := len(p.fake.sent()); n != 3 {
		t.Errorf("only the allowed browsers' messages reached MekBay: %d", n)
	}
}

func TestMekBayCheck(t *testing.T) {
	p := newProxyFixture(t)
	pr := newMekBayProxy(p.store)
	pr.web = p.fake.URL
	if err := pr.Check(context.Background()); err != nil {
		t.Errorf("a MekBay that names its WebSocket the expected way passes: %v", err)
	}
	p.fake.mu.Lock()
	p.fake.scripts = "var M=class{wsUrl=`wss://elsewhere.example/socket`};"
	p.fake.mu.Unlock()
	if err := pr.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "any more") {
		t.Errorf("a MekBay that renamed it fails the check: %v", err)
	}
	pr.web = "http://127.0.0.1:1"
	if err := pr.Check(context.Background()); err == nil {
		t.Error("an unreachable MekBay fails the check")
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

func TestMekBayLinkBuilder(t *testing.T) {
	store, err := OpenSQLite(t.TempDir() + "/builder.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.EnsurePlaythrough(context.Background(), "pt"); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store}
	s.proxy = newMekBayProxy(store)
	re := regexp.MustCompile(`^(https?)://mekbay-([a-z2-7]{32})\.(.+?)/(\?.*)?$`)
	for _, c := range []struct{ host, proto, wantScheme, wantSite string }{
		{"battletech.zech.io", "https", "https", "battletech.zech.io"},
		{"localhost:8080", "", "http", "localhost:8080"},
		{"mekbay.localhost:8080", "", "http", "localhost:8080"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Host = c.host
		if c.proto != "" {
			r.Header.Set("X-Forwarded-Proto", c.proto)
		}
		link, err := s.mekbayLink(r, "pt", "camp", "player-1", testAccount, url.Values{"instance": {"force-1"}})
		m := re.FindStringSubmatch(link)
		if err != nil || m == nil || m[1] != c.wantScheme || m[3] != c.wantSite {
			t.Fatalf("%s: %q %v", c.host, link, err)
		}
		u, _ := url.Parse(link)
		if u.Query().Get("instance") != "force-1" || strings.Contains(link, testAccount) {
			t.Errorf("the link opens the force and never shows the account: %q", link)
		}
		// Asking again for the same label gives the very same link.
		if again, _ := s.mekbayLink(r, "pt", "camp", "player-1", testAccount, url.Values{"instance": {"force-1"}}); again != link {
			t.Errorf("a link is stable: %q vs %q", again, link)
		}
		if acct, ok, _ := store.MekBayLink(context.Background(), m[2], time.Now()); !ok || acct != testAccount {
			t.Errorf("the link is in the table")
		}
	}
	if link, _ := s.mekbayLink(httptest.NewRequest("GET", "/", nil), "pt", "camp", "opfor", otherAccount, nil); strings.Contains(link, "instance=") || !strings.HasSuffix(link, "/") {
		t.Errorf("no force, no instance: %q", link)
	}
	if _, err := s.mekbayLink(httptest.NewRequest("GET", "/", nil), "pt", "camp", "bad", "nope", nil); err == nil {
		t.Error("only a real account gets a link")
	}
}

// Deleting a campaign takes its MekBay links with it.
func TestDeletingTheCampaignRevokesItsLinks(t *testing.T) {
	srv, store, cookie := startedCompany(t)
	ctx := context.Background()
	id, err := store.EnsureMekBayLink(ctx, playthroughID("wolfpack2026"), "refusalwar", tableLabel, accountUID("wolfpack2026", SlotHangar), time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if r := post(t, srv, "/api/campaign/delete", url.Values{"campaign": {"refusalwar"}}, cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", r.StatusCode)
	}
	if _, ok, _ := store.MekBayLink(ctx, id, time.Now()); ok {
		t.Error("deleting the campaign revokes its links")
	}
}
