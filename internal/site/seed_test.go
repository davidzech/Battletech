package site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const (
	goodInstance = "01a12f9e-188f-7158-ad67-3fc9700ec733"
	goodSel      = "01a12f9d-a79b-717d-8c84-d5da73d2f9a5"
	goodHangar   = "https://mekbay.com/?instance=" + goodInstance + "&sel=" + goodSel
)

func TestValidSeed(t *testing.T) {
	for seed, want := range map[string]bool{
		"abcd1234":              true,
		"a1b2c3d4e5f6":          true,
		strings.Repeat("a", 32): true,
		"abc1234":               false, // too short
		strings.Repeat("a", 33): false, // too long
		"ABCD1234":              false, // uppercase
		"abcd-1234":             false, // punctuation
		"abcd 1234":             false, // space
		"":                      false,
		"abcd1234\n":            false,
		"éééééééé":              false,
	} {
		if got := validSeed(seed); got != want {
			t.Errorf("validSeed(%q) = %v, want %v", seed, got, want)
		}
	}
}

func TestPlaythroughIDIsStableAndOpaque(t *testing.T) {
	a, b := playthroughID("abcd1234"), playthroughID("abcd1234")
	if a != b || len(a) != 64 || strings.Contains(a, "abcd1234") {
		t.Fatalf("bad id %q", a)
	}
	if playthroughID("abcd1235") == a {
		t.Fatal("different seeds must give different ids")
	}
}

func TestParseHangar(t *testing.T) {
	good, err := parseHangar("  " + strings.ToUpper(goodHangar[:8]) + goodHangar[8:] + " ")
	if err != nil || good.Instance != goodInstance || good.Sel != goodSel {
		t.Fatalf("good link rejected or misparsed: %+v, %v", good, err)
	}
	if h, err := parseHangar("https://mekbay.com/?instance=" + goodInstance); err != nil || h.Sel != "" {
		t.Fatalf("link without sel should be fine: %+v, %v", h, err)
	}
	for _, bad := range []string{
		"",
		"not a url",
		"http://mekbay.com/?instance=" + goodInstance,    // not https
		"https://evil.example/?instance=" + goodInstance, // wrong host
		"https://mekbay.com.evil.example/?instance=" + goodInstance,
		"https://user@mekbay.com/?instance=" + goodInstance,
		"https://mekbay.com:8443/?instance=" + goodInstance,
		"https://mekbay.com/", // no instance
		"https://mekbay.com/?instance=nope",
		"https://mekbay.com/?instance=" + goodInstance + "&sel=nope",
	} {
		if _, err := parseHangar(bad); err == nil {
			t.Errorf("parseHangar(%q) should fail", bad)
		}
	}
}

func TestParseCompanyName(t *testing.T) {
	for in, want := range map[string]string{
		"Wolf's Dragoons":         "Wolf's Dragoons",
		"  Kell   Hounds ":        "Kell Hounds",
		"Wolf\nDragoons":          "Wolf Dragoons",
		"Wolf’s Dragoons":         "Wolf’s Dragoons",
		"1st Somerset Strikers":   "1st Somerset Strikers",
		"Gray Death Legion (GDL)": "Gray Death Legion (GDL)",
		"Écureuils d'Or":          "Écureuils d'Or",
	} {
		if got, err := parseCompanyName(in); err != nil || got != want {
			t.Errorf("parseCompanyName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "   ", "-Dragoons", "<script>alert(1)</script>", "Wolf\x00Dragoons", strings.Repeat("a", 41), "Dragoons/../"} {
		if _, err := parseCompanyName(bad); err == nil {
			t.Errorf("parseCompanyName(%q) should fail", bad)
		}
	}
}

// newTestServer is a site with a fake MekBay whose hangar meets Refusal War's starting conditions, so tests that
// start campaigns never touch the network.
func newTestServer(t *testing.T) (*httptest.Server, *Store) {
	t.Helper()
	return newMekBayTestServer(t, &fakeMekBay{hangar: startingHangar()})
}

func post(t *testing.T, srv *httptest.Server, path string, form url.Values, cookie *http.Cookie, origin string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func body(t *testing.T, srv *httptest.Server, path string, cookie *http.Cookie) string {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := res.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}

func TestSeedAndCampaignFlow(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	// First visit: no seed, so the page asks for one.
	home := body(t, srv, "/", nil)
	if !strings.Contains(home, `id="seed-dialog"`) || !strings.Contains(home, `data-seed=""`) || !strings.Contains(home, "Set seed") {
		t.Fatal("first visit should offer the seed dialog")
	}

	// A bad seed is refused and nothing is stored.
	if res := post(t, srv, "/api/seed", url.Values{"seed": {"Bad Seed!"}}, nil, ""); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad seed: got %d", res.StatusCode)
	}

	// A good seed sets an HttpOnly cookie, creates the playthrough, and reports it as not yet configured.
	res := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("good seed: got %d", res.StatusCode)
	}
	var cookie *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == "seed" {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value != "wolfpack2026" || !cookie.HttpOnly || cookie.MaxAge <= 0 {
		t.Fatalf("bad seed cookie: %+v", cookie)
	}
	id := playthroughID("wolfpack2026")
	if pt, ok, _ := store.Playthrough(ctx, id); !ok || pt.Configured() {
		t.Fatalf("new playthrough should exist and be unconfigured: %+v (exists=%v)", pt, ok)
	}
	// The stored key must not be the seed.
	var raw string
	_ = store.db.QueryRow(`SELECT id FROM playthroughs`).Scan(&raw)
	if strings.Contains(raw, "wolfpack") {
		t.Fatal("seed must not be stored")
	}

	// With the cookie, the page now asks to finish setup.
	if page := body(t, srv, "/", cookie); !strings.Contains(page, `data-seed="wolfpack2026"`) || !strings.Contains(page, "Finish setup") {
		t.Fatal("seeded but unconfigured page should say Finish setup")
	} else if strings.Contains(page, `id="hangar-link"`) {
		t.Fatal("no hangar link before setup")
	}

	// Naming the company needs a seed cookie and a sensible name.
	if r := post(t, srv, "/api/company", url.Values{"company": {"Wolf's Dragoons"}}, nil, ""); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("company without a seed: got %d", r.StatusCode)
	}
	for _, bad := range []string{"", "   ", "<b>", strings.Repeat("a", 41)} {
		if r := post(t, srv, "/api/company", url.Values{"company": {bad}}, cookie, ""); r.StatusCode != http.StatusBadRequest {
			t.Fatalf("bad company %q: got %d", bad, r.StatusCode)
		}
	}
	if r := post(t, srv, "/api/company", url.Values{"company": {"Wolf's Dragoons"}}, cookie, "https://evil.example"); r.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin: got %d", r.StatusCode)
	}
	if pt, _, _ := store.Playthrough(ctx, id); pt.Configured() {
		t.Fatal("rejected requests must not name the company")
	}
	// A campaign can't be started before the company has a name.
	camp := startForm(goodHangar, "Alrek", "Katarina")
	if r := post(t, srv, "/api/campaign/start", camp, cookie, srv.URL); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("start before naming the company: got %d", r.StatusCode)
	}

	// A good name is saved, tidied.
	if r := post(t, srv, "/api/company", url.Values{"company": {"  Wolf's   Dragoons "}}, cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("good company: got %d", r.StatusCode)
	}
	if pt, _, _ := store.Playthrough(ctx, id); !pt.Configured() || pt.CompanyName != "Wolf's Dragoons" {
		t.Fatalf("company not saved correctly: %+v", pt)
	}

	// Entering the same seed again (say, on another device) finds the named company.
	res = post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "")
	var out strings.Builder
	buf := make([]byte, 256)
	n, _ := res.Body.Read(buf)
	out.Write(buf[:n])
	if !strings.Contains(out.String(), `"configured":true`) {
		t.Fatalf("existing company should report configured: %q", out.String())
	}
	page := body(t, srv, "/", cookie)
	if !strings.Contains(page, `<span class="company">Wolf&#39;s Dragoons</span></button>`) {
		t.Fatalf("configured page should show the company name on the chip: %.1500s", page)
	}
	if strings.Contains(page, "wolf…") {
		t.Fatal("the header chip should not show the seed")
	}
	if strings.Contains(page, `id="hangar-link"`) {
		t.Fatal("no hangar link before a campaign is started")
	}
	if strings.Contains(page, `id="theme"`) {
		t.Fatal("the theme toggle is gone")
	}

	// The campaign page offers to start it.
	if c := body(t, srv, "/refusalwar/", cookie); !strings.Contains(c, `id="wizard-open"`) || !strings.Contains(c, "Start this campaign") || strings.Contains(c, "Campaign started") {
		t.Fatalf("unstarted campaign page: %.1500s", c)
	}

	// Starting needs a seed, the same origin, a known campaign and a valid hangar link.
	for name, c := range map[string]struct {
		form   url.Values
		cookie *http.Cookie
		origin string
		status int
	}{
		"no seed":          {camp, nil, srv.URL, http.StatusBadRequest},
		"cross-origin":     {camp, cookie, "https://evil.example", http.StatusForbidden},
		"unknown campaign": {url.Values{"campaign": {"nope"}, "hangar": {goodHangar}}, cookie, srv.URL, http.StatusNotFound},
		"bad hangar":       {url.Values{"campaign": {"refusalwar"}, "hangar": {"https://evil.example/?instance=" + goodInstance}}, cookie, srv.URL, http.StatusBadRequest},
	} {
		if r := post(t, srv, "/api/campaign/start", c.form, c.cookie, c.origin); r.StatusCode != c.status {
			t.Errorf("start, %s: got %d, want %d", name, r.StatusCode, c.status)
		}
	}
	if _, ok, _ := store.Run(ctx, id, "refusalwar"); ok {
		t.Fatal("rejected starts must not start the campaign")
	}

	// A valid start records the campaign's own hangar.
	if r := post(t, srv, "/api/campaign/start", camp, cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("good start: got %d", r.StatusCode)
	}
	run, ok, _ := store.Run(ctx, id, "refusalwar")
	if !ok || !run.Started() || run.HangarInstance != goodInstance || run.HangarSel != goodSel || run.HangarURL != goodHangar || run.Campaign != "refusalwar" {
		t.Fatalf("campaign not started correctly: %+v (started=%v)", run, ok)
	}
	if _, ok, _ := store.Run(ctx, id, "another-campaign"); ok {
		t.Fatal("starting one campaign must not start another")
	}

	// That campaign's pages link straight to its hangar; other pages don't.
	link := `id="hangar-link" href="` + strings.ReplaceAll(goodHangar, "&", "&amp;") + `" target="_blank" rel="noopener noreferrer"`
	for _, path := range []string{"/refusalwar/", "/refusalwar/scenario_000/"} {
		if !strings.Contains(body(t, srv, path, cookie), link) {
			t.Errorf("%s should link to the hangar", path)
		}
	}
	if strings.Contains(body(t, srv, "/", cookie), `id="hangar-link"`) {
		t.Error("the home page isn't in a campaign, so it has no hangar link")
	}
	if c := body(t, srv, "/refusalwar/", cookie); !strings.Contains(c, "Campaign started") || !strings.Contains(c, "Change hangar link") {
		t.Errorf("started campaign page: %.1500s", c)
	}

	// Starting again changes the hangar link and keeps a single run.
	other := "https://mekbay.com/?instance=01a12f98-8a21-7d12-b43d-52bb70872376"
	if r := post(t, srv, "/api/campaign/start", url.Values{"campaign": {"refusalwar"}, "hangar": {other}}, cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("change hangar: got %d", r.StatusCode)
	}
	var runs int
	_ = store.db.QueryRow(`SELECT COUNT(*) FROM campaign_runs`).Scan(&runs)
	if run, _, _ := store.Run(ctx, id, "refusalwar"); runs != 1 || run.HangarInstance != "01a12f98-8a21-7d12-b43d-52bb70872376" || run.HangarSel != "" {
		t.Fatalf("changing the hangar should update the one run: %d runs, %+v", runs, run)
	}
}

func TestParseOwnerUID(t *testing.T) {
	const uid = "e66a3544-8fcf-4dab-955a-f4ea2db0b610"
	if got, err := parseOwnerUID("  " + strings.ToUpper(uid) + " "); err != nil || got != uid {
		t.Errorf("parseOwnerUID = %q, %v", got, err)
	}
	if got, err := parseOwnerUID(""); err != nil || got != "" {
		t.Errorf("blank should be allowed, got %q, %v", got, err)
	}
	for _, bad := range []string{"nope", uid + "x", "https://mekbay.com/?instance=" + goodInstance} {
		if _, err := parseOwnerUID(bad); err == nil {
			t.Errorf("parseOwnerUID(%q) should fail", bad)
		}
	}
}

func TestModifyModeIsOptionalAndPrivate(t *testing.T) {
	const uid = "e66a3544-8fcf-4dab-955a-f4ea2db0b610"
	srv, store := newTestServer(t)
	ctx := context.Background()
	res := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "")
	cookie := res.Cookies()[0]
	id := playthroughID("wolfpack2026")
	setup := func(extra url.Values) int {
		v := url.Values{"company": {"Wolf's Dragoons"}}
		for k, vs := range extra {
			v[k] = vs
		}
		return post(t, srv, "/api/company", v, cookie, srv.URL).StatusCode
	}

	// Without a UID the campaign is read-only.
	if c := setup(nil); c != http.StatusNoContent {
		t.Fatalf("setup without owner: %d", c)
	}
	if pt, _, _ := store.Playthrough(ctx, id); pt.CanModify() {
		t.Fatal("no UID should mean read-only")
	}
	// A malformed UID is refused.
	if c := setup(url.Values{"owner": {"not-a-uid"}}); c != http.StatusBadRequest {
		t.Fatalf("bad owner: %d", c)
	}
	// A valid UID turns modify mode on, and the page never echoes it back.
	if c := setup(url.Values{"owner": {uid}}); c != http.StatusNoContent {
		t.Fatalf("good owner: %d", c)
	}
	if pt, _, _ := store.Playthrough(ctx, id); !pt.CanModify() || pt.OwnerUID != uid {
		t.Fatalf("modify mode not saved: %+v", pt)
	}
	page := body(t, srv, "/", cookie)
	if strings.Contains(page, uid) || !strings.Contains(page, "Modify mode is on.") {
		t.Fatal("page should say modify mode is on without revealing the UID")
	}
	// Saving again with the field blank keeps it; the checkbox clears it.
	if c := setup(nil); c != http.StatusNoContent {
		t.Fatalf("resave: %d", c)
	}
	if pt, _, _ := store.Playthrough(ctx, id); pt.OwnerUID != uid {
		t.Fatal("a blank owner field should keep the saved UID")
	}
	if c := setup(url.Values{"clear_owner": {"1"}}); c != http.StatusNoContent {
		t.Fatalf("clear: %d", c)
	}
	if pt, _, _ := store.Playthrough(ctx, id); pt.CanModify() {
		t.Fatal("clear_owner should turn modify mode off")
	}
}

func TestInvalidCookieIsIgnored(t *testing.T) {
	srv, _ := newTestServer(t)
	page := body(t, srv, "/", &http.Cookie{Name: "seed", Value: "NOT VALID"})
	if !strings.Contains(page, `data-seed=""`) {
		t.Fatal("a malformed seed cookie should be treated as no seed")
	}
}

func TestStaticFilesRevalidate(t *testing.T) {
	srv, _ := newTestServer(t)
	res, err := http.Get(srv.URL + "/static/bt_oldstyle.ttf")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	tag := res.Header.Get("ETag")
	if tag == "" {
		t.Fatal("static file served without an ETag")
	}
	req, _ := http.NewRequest("GET", srv.URL+"/static/bt_oldstyle.ttf", nil)
	req.Header.Set("If-None-Match", tag)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotModified {
		t.Fatalf("revalidating with the ETag gave %d, want 304", res.StatusCode)
	}
}

func TestShortCommit(t *testing.T) {
	for in, want := range map[string]string{
		"dev":     "dev",
		"e42dcb5": "e42dcb5",
		"e42dcb519e167135fb40db03d94a087dbc10261b":       "e42dcb5",
		"e42dcb519e167135fb40db03d94a087dbc10261b-dirty": "e42dcb5-dirty",
	} {
		if got := shortCommit(in); got != want {
			t.Errorf("shortCommit(%q) = %q, want %q", in, got, want)
		}
	}
}
