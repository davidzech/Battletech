package site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
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

func TestParsePlayers(t *testing.T) {
	for in, ok := range map[string]bool{"1": true, "4": true, " 3 ": true, "0": false, "5": false, "-1": false, "x": false, "": false} {
		if _, err := parsePlayers(in); (err == nil) != ok {
			t.Errorf("parsePlayers(%q) ok=%v, want %v", in, err == nil, ok)
		}
	}
}

func newTestServer(t *testing.T) (*httptest.Server, *Store) {
	t.Helper()
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	srv := httptest.NewServer(NewHandler("../../campaigns", store))
	t.Cleanup(srv.Close)
	return srv, store
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

	// Setup needs a seed cookie, a valid hangar and 1-4 players.
	if r := post(t, srv, "/api/campaign", url.Values{"hangar": {goodHangar}, "players": {"2"}}, nil, ""); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("setup without a seed: got %d", r.StatusCode)
	}
	if r := post(t, srv, "/api/campaign", url.Values{"hangar": {"https://evil.example/?instance=" + goodInstance}, "players": {"2"}}, cookie, ""); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad hangar: got %d", r.StatusCode)
	}
	if r := post(t, srv, "/api/campaign", url.Values{"hangar": {goodHangar}, "players": {"5"}}, cookie, ""); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad player count: got %d", r.StatusCode)
	}
	if pt, _, _ := store.Playthrough(ctx, id); pt.Configured() {
		t.Fatal("rejected setups must not be saved")
	}

	// Cross-origin posts are refused.
	if r := post(t, srv, "/api/campaign", url.Values{"hangar": {goodHangar}, "players": {"2"}}, cookie, "https://evil.example"); r.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin: got %d", r.StatusCode)
	}

	// A valid setup is saved.
	if r := post(t, srv, "/api/campaign", url.Values{"hangar": {goodHangar}, "players": {"3"}}, cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("good setup: got %d", r.StatusCode)
	}
	pt, _, _ := store.Playthrough(ctx, id)
	if !pt.Configured() || pt.Players != 3 || pt.HangarInstance != goodInstance || pt.HangarSel != goodSel || pt.HangarURL != goodHangar {
		t.Fatalf("setup not saved correctly: %+v", pt)
	}

	// Entering the same seed again (say, on another device) finds the finished setup.
	res = post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "")
	var out strings.Builder
	buf := make([]byte, 256)
	n, _ := res.Body.Read(buf)
	out.Write(buf[:n])
	if !strings.Contains(out.String(), `"configured":true`) {
		t.Fatalf("existing campaign should report configured: %q", out.String())
	}
	page := body(t, srv, "/", cookie)
	if !strings.Contains(page, "Seed <code>wolf…</code>") {
		t.Fatal("configured page should show the seed chip")
	}
	// The header links straight to the player's MekBay hangar.
	if !strings.Contains(page, `id="hangar-link" href="`+strings.ReplaceAll(goodHangar, "&", "&amp;")+`" target="_blank" rel="noopener noreferrer"`) {
		t.Fatalf("configured page should link to the hangar, got: %s", page[:min(len(page), 1800)])
	}
	if strings.Contains(page, `id="theme"`) {
		t.Fatal("the theme toggle is gone")
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
