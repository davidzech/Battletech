package site

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// The MekBay proxy: MekBay's own web app, served from a host of ours (mekbay.<our host>), acting as the MekBay account a link names.
//
// Why: MekBay keeps a visitor's identity (a UUID) in the browser's IndexedDB, and an account owns its forces. A force owned by an account
// nobody holds (the site's OpFor, a player's derived account) would be read-only in a stock browser. The proxy lets the site hand a player
// a plain link that opens MekBay already acting as the right account, with nothing to paste and nothing to lose.
//
// How: two parts, both in this program (MekBay's server is closed source and its front end is a stack we don't want to host or fork):
//
//   - The web app. MekBay's prebuilt static files are fetched from mekbay.com on demand, cached and served unchanged except for two things:
//     the one JavaScript string that names its WebSocket (wss://mekbay.com/ws), which now names our relay, and the service workers
//     (ngsw-worker.js, safety-worker.js), which become stubs that unregister themselves, because a rewritten file would fail the worker's own
//     hash checks. Everything else is untouched, so the app is always MekBay's current build and stays in step with its server.
//   - The relay (/ws). It accepts the browser's WebSocket, opens one to mekbay.com and passes the messages through, replacing the "uuid" of
//     every message the browser sends with the account its token names. The browser still believes it is a random visitor.
//
// The identity comes from a link token (mekbaytoken.go): ?t=<token> on any page sets a host-only, HttpOnly cookie and redirects without it.
// A browser with no cookie is refused, except on a developer's machine (sandboxAllowed), where it acts as one shared sandbox account so the
// proxy host also works when opened directly. A cookie that is present but invalid is always refused. The site's own cookies never reach this host (it
// is a different origin from the site), which also keeps MekBay's JavaScript away from the site's APIs.
//
// This relies on MekBay's unofficial, unpublished protocol and on the WebSocket string surviving its build; Check says whether it does.

const (
	mekbayWebURL    = "https://mekbay.com"
	mekbayWSURL     = "wss://mekbay.com/ws"
	mekbayOriginURL = "https://mekbay.com"
	mekbayCookie    = "mekbay_id"

	// mekbayWSLiteral is the string in MekBay's compiled JavaScript that the proxy replaces.
	mekbayWSLiteral = "wss://mekbay.com/ws"

	// proxyCacheEntries bounds the file cache so arbitrary request paths can't grow it forever.
	proxyCacheEntries = 1000
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

type mekbayProxy struct {
	web, ws, origin string
	client          *http.Client
	tokens          *mekbayTokens
	now             func() time.Time

	mu    sync.Mutex
	cache map[string]*proxiedFile

	rewrites atomic.Int64 // JavaScript files served with the WebSocket string replaced
}

type proxiedFile struct {
	status      int
	contentType string
	body        []byte
	fetched     time.Time
}

func newMekBayProxy(tokens *mekbayTokens) *mekbayProxy {
	return &mekbayProxy{
		web: mekbayWebURL, ws: mekbayWSURL, origin: mekbayOriginURL,
		client: &http.Client{Timeout: 30 * time.Second},
		tokens: tokens,
		now:    time.Now,
		cache:  map[string]*proxiedFile{},
	}
}

// MekBayLinkToken seals a MekBay account into a link token under the server's secret (MEKBAY_TOKEN_KEY), for building the links the site
// hands out: https://mekbay.<host>/?t=<token>&instance=<force>.
func MekBayLinkToken(secret, account string, ttl time.Duration) (string, error) {
	if secret == "" {
		return "", errors.New("no MekBay token key is set")
	}
	return newMekBayTokens(secret).Issue(account, ttl, time.Now())
}

// isMekBayHost reports whether a request's host is the MekBay proxy's (mekbay.<anything>, with or without a port).
func isMekBayHost(host string) bool {
	if i := strings.LastIndex(host, ":"); i >= 0 && !strings.Contains(host[i:], "]") {
		host = host[:i]
	}
	return strings.HasPrefix(strings.ToLower(host), "mekbay.")
}

// sandboxAllowed says whether a browser with no token may use the shared sandbox account: only when the proxy is reached as *.localhost
// (mekbay.localhost:8080) by a client on the same machine. The Host header alone proves nothing, since a remote client picks its own, so the
// connection must also come from a loopback address and carry no forwarding header (behind the host's proxy it never does).
func sandboxAllowed(r *http.Request) bool {
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

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (p *mekbayProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/ws":
		p.relay(w, r)
		return
	case "/ngsw-worker.js", "/safety-worker.js":
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Cache-Control", "no-store")
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
	if tok := r.URL.Query().Get("t"); tok != "" {
		p.acceptToken(w, r, tok)
		return
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
	h := w.Header()
	h.Set("Content-Type", f.contentType)
	if strings.HasPrefix(f.contentType, "text/html") {
		h.Set("Cache-Control", "no-cache")
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
	if isHTTPS(r) {
		scheme = "wss"
	}
	return scheme + "://" + r.Host + "/ws"
}

func isJavaScript(contentType string) bool {
	return strings.Contains(contentType, "javascript")
}

// acceptToken stores a link's token in a host-only cookie and redirects to the same page without it.
func (p *mekbayProxy) acceptToken(w http.ResponseWriter, r *http.Request, tok string) {
	_, expires, err := p.tokens.Open(tok, p.now())
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		http.Error(w, ErrBadToken.Error(), http.StatusForbidden)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: mekbayCookie, Value: tok, Path: "/", Expires: expires,
		HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
	q := r.URL.Query()
	q.Del("t")
	u := url.URL{Path: r.URL.Path, RawQuery: q.Encode()}
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

// file returns an upstream file, from the cache when it is fresh and from the last copy when MekBay can't be reached.
func (p *mekbayProxy) file(ctx context.Context, path string) (*proxiedFile, error) {
	p.mu.Lock()
	f := p.cache[path]
	p.mu.Unlock()
	if f != nil && p.now().Sub(f.fetched) < fileTTL(f) {
		return f, nil
	}
	nf, err := p.download(ctx, path)
	if err != nil {
		if f != nil {
			return f, nil // stale beats nothing
		}
		return nil, err
	}
	if nf.status < 500 {
		p.mu.Lock()
		if _, known := p.cache[path]; known || len(p.cache) < proxyCacheEntries {
			p.cache[path] = nf
		}
		p.mu.Unlock()
	}
	return nf, nil
}

// fileTTL: the page itself changes with each MekBay release, its hashed files never do.
func fileTTL(f *proxiedFile) time.Duration {
	if strings.HasPrefix(f.contentType, "text/html") {
		return time.Minute
	}
	return time.Hour
}

func (p *mekbayProxy) download(ctx context.Context, path string) (*proxiedFile, error) {
	base, err := url.Parse(p.web)
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
	return &proxiedFile{status: resp.StatusCode, contentType: ct, body: body, fetched: p.now()}, nil
}

// relay passes one browser's WebSocket to mekbay.com, speaking as the account its cookie's token names.
func (p *mekbayProxy) relay(w http.ResponseWriter, r *http.Request) {
	// A browser with a token cookie acts as that account (a cookie that is present but not valid is refused, so nobody believes they are acting
	// as themselves when they aren't). A browser with none is a visitor who opened the proxy directly: on a developer's machine it acts as
	// the shared sandbox account, so MekBay works there as an open stomping ground; anywhere else it is refused, so the deployed site is never
	// an open door to MekBay.
	var account string
	if c, err := r.Cookie(mekbayCookie); err == nil {
		id, _, err := p.tokens.Open(c.Value, p.now())
		if err != nil {
			http.Error(w, ErrBadToken.Error(), http.StatusUnauthorized)
			return
		}
		account = id
	} else if sandboxAllowed(r) {
		account = p.tokens.sandbox
	} else {
		http.Error(w, "Open your MekBay link from the site first.", http.StatusUnauthorized)
		return
	}
	// The default origin check (the page's origin must be this host) stops other sites from using a visitor's cookie.
	browser, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept has already answered
	}
	defer browser.CloseNow()
	browser.SetReadLimit(16 << 20)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	up, _, err := websocket.Dial(ctx, p.ws, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {p.origin}}})
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
	err = <-done
	cancel()
	_ = browser.Close(websocket.CloseStatus(err), "")
	_ = up.Close(websocket.CloseStatus(err), "")
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

var scriptAsset = regexp.MustCompile(`^/[A-Za-z0-9._-]+\.js$`)

// Check says whether the proxy can still do its job against the live MekBay: its asset list is readable and at least one of its
// JavaScript files holds the WebSocket string the proxy replaces. A MekBay release that changes how that address is written makes this fail,
// which is the moment to look at the proxy instead of finding out from a player.
func (p *mekbayProxy) Check(ctx context.Context) error {
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

// WithMekBayTokenKey sets the secret that link tokens are sealed under (MEKBAY_TOKEN_KEY). Without one the key is random per process.
func WithMekBayTokenKey(secret string) Option { return func(s *Server) { s.tokenKey = secret } }

// withMekBayUpstream points the proxy at another MekBay (tests use a fake).
func withMekBayUpstream(web, ws, origin string) Option {
	return func(s *Server) { s.upstream = &[3]string{web, ws, origin} }
}

// withMekBayProxyHook hands the proxy to a test once it is built.
func withMekBayProxyHook(f func(*mekbayProxy)) Option { return func(s *Server) { s.onProxy = f } }

// mekbayLink is a link that opens MekBay (the proxy on mekbay.<this host>) acting as account, with the given force loaded when instance is
// set. It carries a fresh token valid for ttl.
func (s *Server) mekbayLink(r *http.Request, account, instance string, ttl time.Duration) (string, error) {
	tok, err := s.proxy.tokens.Issue(account, ttl, s.proxy.now())
	if err != nil {
		return "", err
	}
	scheme := "http"
	if isHTTPS(r) {
		scheme = "https"
	}
	host := r.Host
	if !isMekBayHost(host) {
		host = "mekbay." + host
	}
	q := url.Values{"t": {tok}}
	if instance != "" {
		q.Set("instance", instance)
	}
	return scheme + "://" + host + "/?" + q.Encode(), nil
}
