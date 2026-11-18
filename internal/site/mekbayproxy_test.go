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
	testAccount = "0f1e2d3c-4b5a-4968-8776-655443322110"
	testHost    = "mekbay.localhost"
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
	count := func(w http.ResponseWriter, path string) {
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
		count(w, r.URL.Path)
		_, _ = io.WriteString(w, `{"assetGroups":[{"name":"app","urls":["/index.html","/main-1.js","/chunk-abc.js","/styles.css"]}]}`)
	})
	mux.HandleFunc("/main-1.js", func(w http.ResponseWriter, r *http.Request) {
		count(w, r.URL.Path)
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = io.WriteString(w, "console.log('main');")
	})
	mux.HandleFunc("/chunk-abc.js", func(w http.ResponseWriter, r *http.Request) {
		count(w, r.URL.Path)
		w.Header().Set("Content-Type", "text/javascript")
		f.mu.Lock()
		s := f.scripts
		f.mu.Unlock()
		_, _ = io.WriteString(w, s)
	})
	mux.HandleFunc("/ngsw-worker.js", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "REAL WORKER") })
	mux.HandleFunc("/assets/data.json", func(w http.ResponseWriter, r *http.Request) {
		count(w, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { // the SPA page, also the fallback for every other path
		count(w, "/")
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

// proxyFixture is the site with its MekBay proxy pointed at a fake MekBay, and a client that reaches it as mekbay.localhost.
type proxyFixture struct {
	fake   *fakeUpstream
	site   *httptest.Server
	proxy  *mekbayProxy
	tokens *mekbayTokens
	client *http.Client
}

func newProxyFixture(t *testing.T) *proxyFixture {
	t.Helper()
	fake := newFakeUpstream(t)
	store, err := OpenSQLite(t.TempDir() + "/proxy.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	var proxy *mekbayProxy
	h := NewHandler(t.TempDir(), store, WithMekBayTokenKey("test-key"),
		withMekBayUpstream(fake.URL, "ws"+strings.TrimPrefix(fake.URL, "http")+"/ws", "https://mekbay.com"),
		withMekBayProxyHook(func(p *mekbayProxy) { proxy = p }))
	site := httptest.NewServer(h)
	t.Cleanup(site.Close)
	addr := strings.TrimPrefix(site.URL, "http://")
	dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	client := &http.Client{
		Transport:     &http.Transport{DialContext: dial},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &proxyFixture{fake: fake, site: site, proxy: proxy, tokens: newMekBayTokens("test-key"), client: client}
}

func (p *proxyFixture) base() string { return "http://" + testHost + ":" + portOf(p.site.URL) }

func portOf(u string) string {
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(u, "http://"))
	return port
}

func (p *proxyFixture) get(t *testing.T, path string, cookie *http.Cookie) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", p.base()+path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func (p *proxyFixture) cookie(t *testing.T, account string, ttl time.Duration) *http.Cookie {
	t.Helper()
	tok, err := p.tokens.Issue(account, ttl, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: mekbayCookie, Value: tok}
}

func (p *proxyFixture) dialRelay(t *testing.T, cookie *http.Cookie, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	header := http.Header{}
	if cookie != nil {
		header.Set("Cookie", cookie.String())
	}
	if origin == "" {
		origin = p.base()
	}
	header.Set("Origin", origin)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return websocket.Dial(ctx, "ws://"+testHost+":"+portOf(p.site.URL)+"/ws", &websocket.DialOptions{HTTPClient: p.client, HTTPHeader: header})
}

func TestMekBayHostRouting(t *testing.T) {
	for host, want := range map[string]bool{
		"mekbay.battletech.zech.io": true, "mekbay.localhost:8080": true, "MekBay.Example.com": true,
		"battletech.zech.io": false, "localhost:8080": false, "notmekbay.example.com": false, "mekbay": false,
	} {
		if got := isMekBayHost(host); got != want {
			t.Errorf("isMekBayHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestMekBayProxyServesAndRewrites(t *testing.T) {
	p := newProxyFixture(t)

	resp, body := p.get(t, "/", nil)
	if resp.StatusCode != 200 || !strings.Contains(body, `<base href="/">`) || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("the page is MekBay's own: %d %q", resp.StatusCode, body)
	}
	// An unknown path falls back to the page, as MekBay does.
	if _, body := p.get(t, "/some/route", nil); !strings.Contains(body, "main-1.js") {
		t.Errorf("an unknown path serves the page: %q", body)
	}

	// The script that names the WebSocket now names ours; the rest of it is untouched.
	_, js := p.get(t, "/chunk-abc.js", nil)
	if want := "var M=class{ws=null;wsUrl=`ws://" + testHost + ":" + portOf(p.site.URL) + "/ws`;x=1};"; js != want {
		t.Errorf("the WebSocket address is replaced:\n got %q\nwant %q", js, want)
	}
	if strings.Contains(js, "mekbay.com") {
		t.Error("nothing still names mekbay.com")
	}
	if _, js := p.get(t, "/main-1.js", nil); js != "console.log('main');" {
		t.Errorf("a script without the address is untouched: %q", js)
	}
	// Behind TLS (X-Forwarded-Proto) the address is wss.
	req, _ := http.NewRequest("GET", p.base()+"/chunk-abc.js", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	resp2, err := p.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if !strings.Contains(string(b), "`wss://"+testHost) {
		t.Errorf("behind https the address is wss: %q", b)
	}
	// Data files pass through.
	if _, body := p.get(t, "/assets/data.json", nil); body != `{"ok":true}` {
		t.Errorf("data passes through: %q", body)
	}

	// The service workers are stubs that unregister themselves, and the manifest that would feed them is gone.
	for _, path := range []string{"/ngsw-worker.js", "/safety-worker.js"} {
		resp, body := p.get(t, path, nil)
		if resp.StatusCode != 200 || !strings.Contains(body, "unregister()") || strings.Contains(body, "REAL WORKER") {
			t.Errorf("%s is the stub: %d %q", path, resp.StatusCode, body)
		}
	}
	if resp, _ := p.get(t, "/ngsw.json", nil); resp.StatusCode != 404 {
		t.Errorf("the service worker manifest is gone: %d", resp.StatusCode)
	}

	// Only reading is allowed.
	req, _ = http.NewRequest("POST", p.base()+"/", strings.NewReader("x"))
	if resp, err := p.client.Do(req); err != nil || resp.StatusCode != 405 {
		t.Errorf("POST is refused: %v %v", resp, err)
	}
}

func TestMekBayProxyCachesAndSurvivesAnOutage(t *testing.T) {
	p := newProxyFixture(t)
	for i := 0; i < 3; i++ {
		p.get(t, "/assets/data.json", nil)
	}
	if n := p.fake.hits["/assets/data.json"]; n != 1 {
		t.Errorf("a fresh file is fetched once, not %d times", n)
	}
	// Let the copy go stale and take MekBay down: the old copy is served instead of an error.
	p.proxy.now = func() time.Time { return time.Now().Add(3 * time.Hour) }
	p.fake.Close()
	resp, body := p.get(t, "/assets/data.json", nil)
	if resp.StatusCode != 200 || body != `{"ok":true}` {
		t.Errorf("the last copy is served while MekBay is down: %d %q", resp.StatusCode, body)
	}
	if resp, _ := p.get(t, "/never-seen.js", nil); resp.StatusCode != 502 {
		t.Errorf("a file never seen is a 502 while MekBay is down: %d", resp.StatusCode)
	}
}

func TestMekBayProxyTokenCookie(t *testing.T) {
	p := newProxyFixture(t)
	tok := p.cookie(t, testAccount, time.Hour).Value

	resp, _ := p.get(t, "/?instance=abc&t="+url.QueryEscape(tok)+"&sel=xyz", nil)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("a link with a token redirects: %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/?instance=abc&sel=xyz" {
		t.Errorf("the redirect drops the token and keeps the rest: %q", loc)
	}
	var c *http.Cookie
	for _, k := range resp.Cookies() {
		if k.Name == mekbayCookie {
			c = k
		}
	}
	if c == nil || !c.HttpOnly || c.Value != tok || c.Path != "/" || c.SameSite != http.SameSiteLaxMode || c.Secure {
		t.Errorf("the token is kept in a host-only HttpOnly cookie (Secure only over https): %+v", c)
	}
	if strings.Contains(resp.Header.Get("Location"), tok) || resp.Header.Get("Cache-Control") != "no-store" {
		t.Error("the token isn't left in the URL or cached")
	}

	// Over https the cookie is Secure.
	req, _ := http.NewRequest("GET", p.base()+"/?t="+url.QueryEscape(tok), nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	r2, _ := p.client.Do(req)
	r2.Body.Close()
	if cs := r2.Cookies(); len(cs) != 1 || !cs[0].Secure {
		t.Errorf("the cookie is Secure behind https: %+v", cs)
	}

	for name, bad := range map[string]string{"garbage": "not-a-token", "expired": p.cookie(t, testAccount, -time.Minute).Value, "other key": func() string {
		s, _ := newMekBayTokens("other").Issue(testAccount, time.Hour, time.Now())
		return s
	}()} {
		resp, _ := p.get(t, "/?t="+url.QueryEscape(bad), nil)
		if resp.StatusCode != http.StatusForbidden || len(resp.Cookies()) != 0 {
			t.Errorf("a %s token is refused with no cookie set: %d", name, resp.StatusCode)
		}
	}
}

func TestMekBayRelaySwapsTheIdentity(t *testing.T) {
	p := newProxyFixture(t)
	conn, _, err := p.dialRelay(t, p.cookie(t, testAccount, time.Hour), "")
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

func TestMekBayRelayRefusesStrangers(t *testing.T) {
	p := newProxyFixture(t)
	for name, c := range map[string]*http.Cookie{
		"no cookie":      nil,
		"a bad cookie":   {Name: mekbayCookie, Value: "nope"},
		"expired cookie": p.cookie(t, testAccount, -time.Minute),
	} {
		_, resp, err := p.dialRelay(t, c, "")
		if err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s is refused with 401: %v %v", name, resp, err)
		}
	}
	// Another site can't use a visitor's cookie.
	_, resp, err := p.dialRelay(t, p.cookie(t, testAccount, time.Hour), "https://evil.example")
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("a page from another origin is refused: %v %v", resp, err)
	}
	if n := len(p.fake.sent()); n != 0 {
		t.Errorf("nothing reached MekBay: %d", n)
	}
}

func TestMekBayCheck(t *testing.T) {
	p := newProxyFixture(t)
	pr := newMekBayProxy(newMekBayTokens("k"))
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

func TestMekBayTokens(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	tk := newMekBayTokens("secret")
	tok, err := tk.Issue(strings.ToUpper(testAccount), time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tok, testAccount) || strings.ContainsAny(tok, "+/=") {
		t.Errorf("a token hides the account and is URL-safe: %q", tok)
	}
	if id, exp, err := tk.Open(tok, now.Add(59*time.Minute)); err != nil || id != testAccount || !exp.Equal(now.Add(time.Hour)) {
		t.Errorf("a token opens to its account until it expires: %q %v %v", id, exp, err)
	}
	if _, _, err := tk.Open(tok, now.Add(time.Hour)); err != ErrBadToken {
		t.Errorf("a token expires: %v", err)
	}
	if _, _, err := newMekBayTokens("other").Open(tok, now); err != ErrBadToken {
		t.Errorf("another key can't open it: %v", err)
	}
	flipped := []byte(tok)
	flipped[len(flipped)/2] ^= 1
	for name, bad := range map[string]string{"altered": string(flipped), "empty": "", "short": "abc", "not base64": "%%%"} {
		if _, _, err := tk.Open(bad, now); err != ErrBadToken {
			t.Errorf("%s token: %v", name, err)
		}
	}
	if _, err := tk.Issue("not-a-uuid", time.Hour, now); err == nil {
		t.Error("only a MekBay user ID can be sealed")
	}
	a, _ := tk.Issue(testAccount, time.Hour, now)
	b, _ := tk.Issue(testAccount, time.Hour, now)
	if a == b {
		t.Error("each token is sealed with a fresh nonce")
	}
	if _, err := MekBayLinkToken("", testAccount, time.Hour); err == nil {
		t.Error("tokens need a configured key")
	}
	link, err := MekBayLinkToken("secret", testAccount, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if id, _, err := tk.Open(link, time.Now()); err != nil || id != testAccount {
		t.Errorf("the exported helper makes tokens the proxy accepts: %q %v", id, err)
	}
}

func TestMekBayLinkBuilder(t *testing.T) {
	s := &Server{proxy: newMekBayProxy(newMekBayTokens("k"))}
	for _, c := range []struct{ host, proto, wantPrefix string }{
		{"battletech.zech.io", "https", "https://mekbay.battletech.zech.io/?"},
		{"localhost:8080", "", "http://mekbay.localhost:8080/?"},
		{"mekbay.localhost:8080", "", "http://mekbay.localhost:8080/?"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Host = c.host
		if c.proto != "" {
			r.Header.Set("X-Forwarded-Proto", c.proto)
		}
		link, err := s.mekbayLink(r, testAccount, "force-1", time.Hour)
		if err != nil || !strings.HasPrefix(link, c.wantPrefix) {
			t.Fatalf("%s: %q %v", c.host, link, err)
		}
		u, _ := url.Parse(link)
		if u.Query().Get("instance") != "force-1" {
			t.Errorf("the link opens the force: %q", link)
		}
		if id, _, err := s.proxy.tokens.Open(u.Query().Get("t"), time.Now()); err != nil || id != testAccount {
			t.Errorf("the link's token names the account: %q %v", id, err)
		}
		if strings.Contains(link, testAccount) {
			t.Errorf("the account never appears in the link: %q", link)
		}
	}
	if link, _ := s.mekbayLink(httptest.NewRequest("GET", "/", nil), testAccount, "", time.Hour); strings.Contains(link, "instance=") {
		t.Errorf("no force, no instance: %q", link)
	}
	if _, err := s.mekbayLink(httptest.NewRequest("GET", "/", nil), "nope", "", time.Hour); err == nil {
		t.Error("only a real account gets a link")
	}
}

// The Manage page offers the site's hangar copy editable through the proxy, acting as the account derived from the seed.
func TestManagePageOpensTheHangarEditable(t *testing.T) {
	srv, store, cookie := startedCompany(t)
	page := body(t, srv, "/refusalwar/manage/", cookie)
	m := regexp.MustCompile(`href="(http://mekbay\.[^"]+)"[^>]*>open it editable`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("the advanced note links to the editable hangar: %.300s", page)
	}
	link, _ := url.Parse(strings.ReplaceAll(m[1], "&amp;", "&"))
	run, _, _ := store.Run(context.Background(), playthroughID("wolfpack2026"), "refusalwar")
	if link.Query().Get("instance") != run.HangarInstance || link.Query().Get("t") == "" || strings.Contains(m[1], accountUID("wolfpack2026", SlotHangar)) {
		t.Errorf("the link opens the site's copy without showing the account: %q", m[1])
	}
	// Following it makes this server's proxy remember the account, and strips the token from the address.
	addr := strings.TrimPrefix(srv.URL, "http://")
	client := &http.Client{
		Transport: &http.Transport{DialContext: func(ctx context.Context, n, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, n, addr)
		}},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Get(link.String())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || strings.Contains(resp.Header.Get("Location"), "t=") || len(resp.Cookies()) != 1 || resp.Cookies()[0].Name != mekbayCookie {
		t.Errorf("the token is accepted by the same server: %d %q %v", resp.StatusCode, resp.Header.Get("Location"), resp.Cookies())
	}
	// A run from before the site copied hangars has nothing to open.
	_, _ = store.db.Exec(`UPDATE campaign_runs SET source_url = ''`)
	if page := body(t, srv, "/refusalwar/manage/", cookie); strings.Contains(page, "open it editable") {
		t.Error("without a site-owned hangar there is nothing to open editable")
	}
}
