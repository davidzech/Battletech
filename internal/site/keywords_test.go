package site

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/content"
)

// Edit keywords (the Manage page's Danger zone) sets the ledger to exactly the ticked keywords: kept ones keep the
// scenario that granted them, and only keywords Keywords.md registers are accepted. The pages show each keyword's own
// scenario from the registry, however it got into the ledger.
func TestEditKeywords(t *testing.T) {
	srv, store, cookie := startedCompany(t)
	ctx := context.Background()
	id := playthroughID("wolfpack2026")
	if err := store.CompleteWithOutcome(ctx, id, "refusalwar", "scenario_000", content.OutcomeResult{Pilots: []content.PilotOutcome{}, Keywords: []string{"scorched_earth", "sigint"}}); err != nil {
		t.Fatal(err)
	}
	p := body(t, srv, "/refusalwar/manage/", cookie)
	for _, want := range []string{`id="keywords-open"`, `id="keywords-dialog"`, `value="scorched_earth" checked`, `value="avenger">`, "<b>Counter Intelligence</b>", "The chemical plant was completely destroyed."} {
		if !strings.Contains(p, want) {
			t.Errorf("the manage page's keyword editor is missing %q", want)
		}
	}
	set := func(keywords ...string) *http.Response {
		return post(t, srv, "/api/campaign/keywords", url.Values{"campaign": {"refusalwar"}, "keyword": keywords}, cookie, srv.URL)
	}
	if res := set("scorched_earth", "avenger"); res.StatusCode != http.StatusNoContent {
		t.Fatalf("save: %d", res.StatusCode)
	}
	ledger, _ := store.Ledger(ctx, id, "refusalwar")
	got := map[string]string{}
	for _, e := range ledger {
		got[e.Keyword] = e.Scenario
	}
	if len(got) != 2 || got["scorched_earth"] != "scenario_000" || got["avenger"] != "" {
		t.Errorf("ledger after the edit: %+v", ledger)
	}
	for _, path := range []string{"/refusalwar/", "/refusalwar/manage/"} {
		p := body(t, srv, path, cookie)
		// Scorched Earth was recorded against Scenario 000 above, but its source is 001; Avenger's (002) isn't written yet.
		for _, want := range []string{`data-from="Scenario 001: Pyre Light">Scorched Earth</li>`, `data-from="">Avenger</li>`, `<script src="/static/keywordchips.js"`} {
			if !strings.Contains(p, want) {
				t.Errorf("%s lists the keywords with their cards: missing %q", path, want)
			}
		}
	}
	if res := set("not_a_keyword"); res.StatusCode != http.StatusBadRequest {
		t.Errorf("an unknown keyword: %d", res.StatusCode)
	}
	if res := set(); res.StatusCode != http.StatusNoContent {
		t.Fatalf("clear: %d", res.StatusCode)
	}
	if ledger, _ = store.Ledger(ctx, id, "refusalwar"); len(ledger) != 0 {
		t.Errorf("nothing ticked clears the ledger: %+v", ledger)
	}
	if res := post(t, srv, "/api/campaign/keywords", url.Values{"campaign": {"refusalwar"}, "keyword": {"sigint"}}, cookie, "https://evil.example"); res.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin: %d", res.StatusCode)
	}
}
