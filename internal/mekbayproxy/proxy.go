package mekbayproxy

import (
	"bytes"
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/davidzech/Battletech/internal/storage"
)

const (
	mekbayWebURL    = "https://mekbay.com"
	UpstreamWS      = "wss://mekbay.com/ws"
	mekbayOriginURL = "https://mekbay.com"

	// mekbayWSLiteral is the string in MekBay's compiled JavaScript that the proxy replaces.
	mekbayWSLiteral = "wss://mekbay.com/ws"

	// proxyCacheEntries and proxyCacheBytes bound the file cache, so arbitrary request paths can neither grow it forever nor keep it: when
	// either is passed, the files used longest ago are dropped.
	proxyCacheEntries = 1000
	proxyCacheBytes   = 128 << 20
	// proxyFileLimit is the largest upstream file the proxy will take.
	proxyFileLimit = 64 << 20
)

// mekbayWorkerStub replaces MekBay's service workers: it unregisters itself and empties the caches, so nothing the proxy rewrote is ever
// pinned in a browser and the worker never controls the page.
const mekbayWorkerStub = `self.addEventListener('install', function () { self.skipWaiting(); });
self.addEventListener('activate', function (e) {
  e.waitUntil((async function () {
    try { var keys = await caches.keys(); await Promise.all(keys.map(function (k) { return caches.delete(k); })); } catch (err) {}
    await self.registration.unregister();
  })());
});
`

// linkStore is where the proxy looks links up (the Store; tests may use another).
type linkStore interface {
	MekBayLink(ctx context.Context, id string, now time.Time) (account string, ok bool, err error)
}

type Proxy struct {
	Web, WS, Origin string
	client          *http.Client
	links           linkStore
	Now             func() time.Time

	mu         sync.Mutex
	cache      map[string]*list.Element // path → its element in lru
	lru        *list.List               // of *proxiedFile, most recently used first
	cacheBytes int                      // sum of size() over the cache
	maxEntries int
	maxBytes   int

	rewrites atomic.Int64 // JavaScript files served with the WebSocket string replaced
}

type proxiedFile struct {
	path        string
	status      int
	contentType string
	body        []byte
	fetched     time.Time
}

func New(links linkStore) *Proxy {
	return &Proxy{
		Web: mekbayWebURL, WS: UpstreamWS, Origin: mekbayOriginURL,
		client: &http.Client{Timeout: 30 * time.Second},
		links:  links,
		Now:    time.Now,
		cache:  map[string]*list.Element{},
		lru:    list.New(),

		maxEntries: proxyCacheEntries,
		maxBytes:   proxyCacheBytes,
	}
}

// What a request's host name says about the proxy.
type mekbayHostKind int

const (
	notMekBayHost mekbayHostKind = iota
	// sandboxHost is the bare mekbay.<site host>, which has no link in it.
	sandboxHost
	// linkHost is mekbay-<something>.<site host>; the something may not be a well-formed id.
	linkHost
)

// parseMekBayHost reads a Host header (with or without a port). For a link host it also returns the link id, or "" when what follows
// "mekbay-" is not shaped like one.
func parseMekBayHost(host string) (id string, kind mekbayHostKind) {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	first, _, _ := strings.Cut(strings.ToLower(host), ".")
	switch {
	case first == "mekbay":
		return "", sandboxHost
	case strings.HasPrefix(first, storage.LinkPrefix):
		if id := strings.TrimPrefix(first, storage.LinkPrefix); storage.ValidLinkID(id) {
			return id, linkHost
		}
		return "", linkHost
	}
	return "", notMekBayHost
}

// IsHost reports whether a request's host is one the proxy answers.
func IsHost(host string) bool {
	_, kind := parseMekBayHost(host)
	return kind != notMekBayHost
}

// SiteHost is the site's own host for a request that came to a proxy host (the host without its mekbay label), or the host itself.
func SiteHost(host string) string {
	if !IsHost(host) {
		return host
	}
	_, rest, _ := strings.Cut(host, ".")
	return rest
}

func IsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// SandboxAllowed says whether a browser reached through the bare mekbay.<host> may act as the shared sandbox account: only when the proxy is
// reached as *.localhost by a client on the same machine. The Host header alone proves nothing, since a remote client picks its own, so the
// connection must also come from a loopback address and carry no forwarding header (behind the host's proxy it never does).
func SandboxAllowed(r *http.Request) bool {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if !strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return false
	}
	if r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("Forwarded") != "" || r.Header.Get("X-Forwarded-Host") != "" {
		return false
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	addr := net.ParseIP(ip)
	return addr != nil && addr.IsLoopback()
}

// Account says which MekBay account a request acts as: the account of the link in its host name, or, on a developer's machine reached through
// the bare mekbay.localhost, the shared sandbox. status is 0 when there is one, else the HTTP status to refuse with.
func (p *Proxy) Account(r *http.Request) (account string, status int, msg string) {
	id, kind := parseMekBayHost(r.Host)
	switch {
	case kind == linkHost:
		if id == "" {
			return "", http.StatusForbidden, storage.ErrBadLink.Error()
		}
		acct, ok, err := p.links.MekBayLink(r.Context(), id, p.Now())
		if err != nil {
			log.Printf("mekbay proxy: link lookup: %v", err)
			return "", http.StatusInternalServerError, "Could not look the link up."
		}
		if !ok {
			return "", http.StatusForbidden, storage.ErrBadLink.Error()
		}
		return acct, 0, ""
	case kind == sandboxHost && SandboxAllowed(r):
		return storage.SandboxAccount, 0, ""
	}
	return "", http.StatusForbidden, "Open your MekBay link from the site first."
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Referrer-Policy", "no-referrer") // following a link out of MekBay must never reveal this host name
	h.Set("X-Robots-Tag", "noindex")
	h.Set("X-Content-Type-Options", "nosniff")
	switch r.URL.Path {
	case "/ws":
		p.relay(w, r)
		return
	case "/ngsw-worker.js", "/safety-worker.js":
		h.Set("Content-Type", "application/javascript")
		h.Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, mekbayWorkerStub)
		return
	case "/ngsw.json":
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// The pages (anything that isn't a file) are only for a valid link; the files themselves are public MekBay files.
	if _, kind := parseMekBayHost(r.Host); kind == linkHost && !strings.Contains(path.Base(r.URL.Path), ".") {
		if _, status, msg := p.Account(r); status != 0 {
			h.Set("Cache-Control", "no-store")
			http.Error(w, msg, status)
			return
		}
	}
	f, err := p.file(r.Context(), r.URL.Path)
	if err != nil {
		log.Printf("mekbay proxy: %s: %v", r.URL.Path, err)
		http.Error(w, "MekBay is not reachable right now.", http.StatusBadGateway)
		return
	}
	body := f.body
	if isJavaScript(f.contentType) && bytes.Contains(body, []byte(mekbayWSLiteral)) {
		body = bytes.ReplaceAll(body, []byte(mekbayWSLiteral), []byte(relayURL(r)))
		p.rewrites.Add(1)
	}
	h.Set("Content-Type", f.contentType)
	if strings.HasPrefix(f.contentType, "text/html") {
		h.Set("Cache-Control", "no-store")
	} else {
		h.Set("Cache-Control", "public, max-age=3600")
	}
	if f.status != http.StatusOK {
		w.WriteHeader(f.status)
		if r.Method != http.MethodHead {
			_, _ = w.Write(body)
		}
		return
	}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
}

// relayURL is the WebSocket address the page should use: this host's /ws.
func relayURL(r *http.Request) string {
	scheme := "ws"
	if IsHTTPS(r) {
		scheme = "wss"
	}
	return scheme + "://" + r.Host + "/ws"
}

func isJavaScript(contentType string) bool {
	return strings.Contains(contentType, "javascript")
}

// file returns an upstream file, from the cache when it is fresh and from the last copy when MekBay can't be reached or answers with a
// server error.
func (p *Proxy) file(ctx context.Context, path string) (*proxiedFile, error) {
	f := p.cached(path)
	if f != nil && p.Now().Sub(f.fetched) < fileTTL(f) {
		return f, nil
	}
	nf, err := p.download(ctx, path)
	if err != nil {
		if f != nil {
			return f, nil // stale beats nothing
		}
		return nil, err
	}
	if nf.status >= 500 {
		if f != nil {
			return f, nil // and beats MekBay's error page
		}
		return nf, nil
	}
	p.remember(nf)
	return nf, nil
}

// cached is the cache's copy of a path, fresh or not, or nil. Asking counts as a use.
func (p *Proxy) cached(path string) *proxiedFile {
	p.mu.Lock()
	defer p.mu.Unlock()
	e := p.cache[path]
	if e == nil {
		return nil
	}
	p.lru.MoveToFront(e)
	return e.Value.(*proxiedFile)
}

// remember puts a file in the cache in place of any earlier copy, then drops the files used longest ago until the cache is within its
// limits again. A file too big for the cache on its own is not kept.
func (p *Proxy) remember(f *proxiedFile) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e := p.cache[f.path]; e != nil {
		p.forget(e)
	}
	if f.size() > p.maxBytes {
		return
	}
	p.cache[f.path] = p.lru.PushFront(f)
	p.cacheBytes += f.size()
	for len(p.cache) > p.maxEntries || p.cacheBytes > p.maxBytes {
		p.forget(p.lru.Back())
	}
}

// forget drops one file from the cache; the caller holds mu.
func (p *Proxy) forget(e *list.Element) {
	f := p.lru.Remove(e).(*proxiedFile)
	delete(p.cache, f.path)
	p.cacheBytes -= f.size()
}

// size is what a file counts for against the cache's byte limit. The path counts too: it is the requester's to choose.
func (f *proxiedFile) size() int {
	return len(f.path) + len(f.contentType) + len(f.body)
}

// fileTTL: the page itself changes with each MekBay release, its hashed files never do. An answer that isn't the file (a 404, a redirect)
// is asked for again as soon as the page is, so a path MekBay doesn't have is not held for long.
func fileTTL(f *proxiedFile) time.Duration {
	if f.status != http.StatusOK || strings.HasPrefix(f.contentType, "text/html") {
		return time.Minute
	}
	return time.Hour
}

func (p *Proxy) download(ctx context.Context, path string) (*proxiedFile, error) {
	base, err := url.Parse(p.Web)
	if err != nil {
		return nil, err
	}
	u := base.ResolveReference(&url.URL{Path: path})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; BattletechMekBayProxy)")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, proxyFileLimit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > proxyFileLimit {
		return nil, errors.New("file too large")
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}
	return &proxiedFile{path: path, status: resp.StatusCode, contentType: ct, body: body, fetched: p.Now()}, nil
}

// relay passes one browser's WebSocket to mekbay.com, speaking as the account of the link the browser was reached through.
func (p *Proxy) relay(w http.ResponseWriter, r *http.Request) {
	account, status, msg := p.Account(r)
	if status != 0 {
		http.Error(w, msg, status) // as the pages answer: 403 for a link that isn't good, 500 when it couldn't be looked up
		return
	}
	// The default origin check (the page's origin must be this host) stops another page from using a link's host through a visitor's browser.
	browser, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept has already answered
	}
	defer browser.CloseNow()
	browser.SetReadLimit(16 << 20)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	up, _, err := websocket.Dial(ctx, p.WS, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {p.Origin}}})
	if err != nil {
		log.Printf("mekbay proxy: could not reach MekBay: %v", err)
		_ = browser.Close(websocket.StatusTryAgainLater, "MekBay is not reachable")
		return
	}
	defer up.CloseNow()
	up.SetReadLimit(16 << 20)

	done := make(chan error, 2)
	go func() { done <- pump(ctx, up, browser, nil) }()
	go func() { done <- pump(ctx, browser, up, func(b []byte) []byte { return swapUUID(b, account) }) }()
	code, reason := closeOf(<-done)
	// The side that ended is closed already; the other is told why, with the close handshake, before the context is cancelled: cancelling it first
	// would drop that side's connection (its pump is still reading with the context) and the browser would see an abnormal close in place of MekBay's.
	_ = browser.Close(code, reason)
	_ = up.Close(code, reason)
}

// closeOf is the close code and reason to pass on to the other side when a pump ends with err: the ones the closing side gave, or "going away"
// when its connection just broke.
func closeOf(err error) (websocket.StatusCode, string) {
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		return ce.Code, ce.Reason
	}
	return websocket.StatusGoingAway, ""
}

// pump copies messages from one WebSocket to the other until either side ends, passing text messages through edit when it is set.
func pump(ctx context.Context, from, to *websocket.Conn, edit func([]byte) []byte) error {
	for {
		typ, b, err := from.Read(ctx)
		if err != nil {
			return err
		}
		if edit != nil && typ == websocket.MessageText {
			b = edit(b)
		}
		if err := to.Write(ctx, typ, b); err != nil {
			return err
		}
	}
}

// swapUUID replaces the top-level "uuid" of a MekBay message with the account. Everything else in the message is passed through exactly
// (nested values are never parsed), and a message without a uuid is returned untouched.
func swapUUID(b []byte, account string) []byte {
	if !bytes.Contains(b, []byte(`"uuid"`)) {
		return b
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return b
	}
	if _, ok := m["uuid"]; !ok {
		return b
	}
	m["uuid"] = json.RawMessage(strconv.Quote(account))
	out, err := json.Marshal(m)
	if err != nil {
		return b
	}
	return out
}

// CheckMekBay runs the proxy's Check against the live mekbay.com (see Check). main runs it once at startup, so a MekBay release that breaks the proxy shows in the
// server's log.
func CheckMekBay(ctx context.Context) error {
	return New(nil).Check(ctx)
}

var scriptAsset = regexp.MustCompile(`^/[A-Za-z0-9._-]+\.js$`)

// Check says whether the proxy can still do its job against the live MekBay: its asset list is readable and at least one of its
// JavaScript files holds the WebSocket string the proxy replaces. A MekBay release that changes how that address is written makes this fail,
// which is the moment to look at the proxy instead of finding out from a player.
func (p *Proxy) Check(ctx context.Context) error {
	m, err := p.download(ctx, "/ngsw.json")
	if err != nil || m.status != http.StatusOK {
		return fmt.Errorf("could not read MekBay's asset list: %v (status %d)", err, statusOf(m))
	}
	var manifest struct {
		AssetGroups []struct {
			URLs []string `json:"urls"`
		} `json:"assetGroups"`
	}
	if err := json.Unmarshal(m.body, &manifest); err != nil {
		return fmt.Errorf("MekBay's asset list changed shape: %w", err)
	}
	var files, hits int
	for _, g := range manifest.AssetGroups {
		for _, u := range g.URLs {
			if !scriptAsset.MatchString(u) {
				continue
			}
			files++
			f, err := p.download(ctx, u)
			if err != nil {
				return fmt.Errorf("could not read %s: %w", u, err)
			}
			if bytes.Contains(f.body, []byte(mekbayWSLiteral)) {
				hits++
			}
		}
	}
	if files == 0 {
		return errors.New("MekBay's asset list has no JavaScript files")
	}
	if hits == 0 {
		return fmt.Errorf("none of MekBay's %d JavaScript files contains %q any more", files, mekbayWSLiteral)
	}
	return nil
}

func statusOf(f *proxiedFile) int {
	if f == nil {
		return 0
	}
	return f.status
}
