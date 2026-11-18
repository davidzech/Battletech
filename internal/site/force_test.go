package site

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestForceNames(t *testing.T) {
	for raw, want := range map[string]string{"  Wolf's   Dragoons ": "Wolf's Dragoons", "1st Somerset Strikers": "1st Somerset Strikers", "Kell Hounds": "Kell Hounds"} {
		if got, err := parseForceName(raw); err != nil || got != want {
			t.Errorf("parseForceName(%q) = %q, %v", raw, got, err)
		}
	}
	for _, bad := range []string{"", "   ", "<b>", strings.Repeat("x", 41), "-leading"} {
		if _, err := parseForceName(bad); err == nil {
			t.Errorf("parseForceName(%q) should fail", bad)
		}
	}
	if _, err := parseForceName(""); err == nil || !strings.Contains(err.Error(), "force") {
		t.Errorf("the message says what is being named: %v", err)
	}
	if _, err := parseAccountName(""); err == nil || !strings.Contains(err.Error(), "account") {
		t.Errorf("and for an account: %v", err)
	}
	if (CampaignRun{}).ForceLabel() != "Your force" || (CampaignRun{ForceName: "Kell Hounds"}).ForceLabel() != "Kell Hounds" {
		t.Error("a run from before forces were named is just 'Your force'")
	}
}

func TestStartNeedsAForceNameAndItCanBeRenamed(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	id := playthroughID("wolfpack2026")
	post(t, srv, "/api/account", url.Values{"account": {"My account"}}, cookie, srv.URL)
	start := func(forceName string) int {
		v := startForm(goodHangar, "Alrek", "Katarina")
		if forceName == "" {
			v.Del("force_name")
		} else {
			v.Set("force_name", forceName)
		}
		return post(t, srv, "/api/campaign/start", v, cookie, srv.URL).StatusCode
	}
	for name, bad := range map[string]string{"none sent": "", "blank": "   ", "markup": "<script>"} {
		if got := start(bad); got != http.StatusBadRequest {
			t.Errorf("%s: %d", name, got)
		}
	}
	if _, ok, _ := store.Run(ctx, id, "refusalwar"); ok {
		t.Fatal("a refused start must not start the campaign")
	}
	if got := start("Kell Hounds"); got != http.StatusNoContent {
		t.Fatalf("start: %d", got)
	}
	if run, _, _ := store.Run(ctx, id, "refusalwar"); run.ForceName != "Kell Hounds" {
		t.Fatalf("the force name is saved with the run: %+v", run)
	}
	// The account name is the seed's, and the force name the campaign's: the header shows the account, the campaign pages the force.
	if page := body(t, srv, "/refusalwar/", cookie); !strings.Contains(page, "<h2>Kell Hounds</h2>") || !strings.Contains(page, `<span class="account">My account</span>`) {
		t.Errorf("the campaign page should name the force, the header the account: %.1500s", page)
	}
	man := body(t, srv, "/refusalwar/manage/", cookie)
	if !strings.Contains(man, `id="force-name-value">Kell Hounds</span>`) || !strings.Contains(man, `id="force-name-edit"`) || !strings.Contains(man, "Kell Hounds&rsquo;s hangar holds") {
		t.Errorf("the Manage page shows the force name and lets you rename it: %.800s", man)
	}
	// Renaming a started campaign's force.
	rename := func(name string) int {
		return post(t, srv, "/api/campaign/force-name", url.Values{"campaign": {"refusalwar"}, "name": {name}}, cookie, srv.URL).StatusCode
	}
	if got := post(t, srv, "/api/campaign/force-name", url.Values{"campaign": {"refusalwar"}, "name": {"X"}}, cookie, "https://evil.example").StatusCode; got != http.StatusForbidden {
		t.Errorf("cross-origin: %d", got)
	}
	for _, bad := range []string{"", "  ", strings.Repeat("y", 41)} {
		if got := rename(bad); got != http.StatusBadRequest {
			t.Errorf("rename to %q: %d", bad, got)
		}
	}
	if got := rename("  Gray   Death Legion "); got != http.StatusNoContent {
		t.Fatalf("rename: %d", got)
	}
	if run, _, _ := store.Run(ctx, id, "refusalwar"); run.ForceName != "Gray Death Legion" {
		t.Errorf("renamed: %+v", run)
	}
	// A campaign that was never started has no force to rename.
	if got := post(t, srv, "/api/campaign/force-name", url.Values{"campaign": {"nope"}, "name": {"X"}}, cookie, srv.URL).StatusCode; got != http.StatusNotFound {
		t.Errorf("unknown campaign: %d", got)
	}
}
