package site

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestCampaignLog(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	id := playthroughID("wolfpack2026")
	complete := func(scenario, done string) int {
		if scenario == "scenario_000" && done == "1" { // Scenario 000 has an outcome engine: it is completed by reporting its outcome
			return post(t, srv, "/api/campaign/scenario-outcome/commit", url.Values{"campaign": {"refusalwar"}, "scenario": {scenario}, "inputs": {`{"pilots":[],"mission":{}}`}}, cookie, srv.URL).StatusCode
		}
		return post(t, srv, "/api/campaign/scenario-complete", url.Values{"campaign": {"refusalwar"}, "scenario": {scenario}, "done": {done}}, cookie, srv.URL).StatusCode
	}
	cards := func() string { return body(t, srv, "/refusalwar/", cookie) }

	// Before the account is named or the campaign started, only Scenario 000 shows, and Show all reveals the rest.
	page := cards()
	if !strings.Contains(page, `<li class="available"><a class="card available" href="/refusalwar/scenario_000/">`) || !strings.Contains(page, `<li class="locked"><a class="card locked" href="/refusalwar/scenario_001/">`) {
		t.Fatalf("000 is available and 001 locked: %.1500s", page)
	}
	if !strings.Contains(page, `id="show-all-scenarios"`) || !strings.Contains(page, "Show all Scenarios") || !strings.Contains(page, "/static/scenariolist.js") {
		t.Error("the campaign page offers a Show all Scenarios checkbox while some are locked")
	}
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if got := complete("scenario_000", "1"); got != http.StatusBadRequest {
		t.Errorf("an unstarted campaign has no log: %d", got)
	}
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	if got := post(t, srv, "/api/campaign/scenario-complete", url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_000"}, "done": {"1"}}, cookie, "https://evil.example").StatusCode; got != http.StatusForbidden {
		t.Errorf("cross-origin: %d", got)
	}
	// 001 can't be completed before 000; unknown scenarios are refused.
	if got := complete("scenario_001", "1"); got != http.StatusConflict {
		t.Errorf("completing a locked scenario: %d", got)
	}
	if got := complete("scenario_999", "1"); got != http.StatusNotFound {
		t.Errorf("unknown scenario: %d", got)
	}
	if done, _ := store.Completions(ctx, id, "refusalwar"); len(done) != 0 {
		t.Fatalf("nothing is completed yet: %v", done)
	}
	// The scenario pages carry the mission log: 000 (an outcome scenario) offers Complete mission, 001 says it is not available.
	if p := body(t, srv, "/refusalwar/scenario_000/", cookie); !strings.Contains(p, `id="mission-log"`) || !strings.Contains(p, "Complete mission") || !strings.Contains(p, "Not completed") {
		t.Errorf("Scenario 000's mission log: %.300s", p)
	}
	if p := body(t, srv, "/refusalwar/scenario_001/", cookie); !strings.Contains(p, "Not available yet") || strings.Contains(p, "mission-log-toggle") {
		t.Error("a locked scenario cannot be marked")
	}
	// The breadcrumb menu leaves out locked scenarios.
	if p := body(t, srv, "/refusalwar/scenario_000/", cookie); strings.Contains(p, "001 – Pyre Light") {
		t.Error("the menu should not list a locked scenario")
	}

	// A started campaign's pages have a Manage Force button in the header, except the Manage page itself.
	for _, path := range []string{"/refusalwar/", "/refusalwar/scenario_000/"} {
		if p := body(t, srv, path, cookie); !strings.Contains(p, `class="chip manage-chip" href="/refusalwar/manage/">Manage Force</a>`) {
			t.Errorf("%s: no Manage Force button in the header", path)
		}
	}
	if p := body(t, srv, "/refusalwar/manage/", cookie); strings.Contains(p, "manage-chip") {
		t.Error("the Manage page has no Manage Force button")
	}
	if p := body(t, srv, "/", cookie); strings.Contains(p, "manage-chip") {
		t.Error("outside a campaign there is no Manage Force button")
	}
	// Completing 000 turns its card green and opens 001.
	if got := complete("scenario_000", "1"); got != http.StatusNoContent {
		t.Fatalf("complete 000: %d", got)
	}
	if got := complete("scenario_000", "1"); got != http.StatusConflict { // an outcome is committed once, so the rewards can't be given twice
		t.Errorf("complete again: %d", got)
	}
	if got := complete("scenario_001", "1"); got != http.StatusNoContent || complete("scenario_001", "1") != http.StatusNoContent { // a plain mark is idempotent
		t.Errorf("complete 001 twice: %d", got)
	}
	if got := complete("scenario_001", "0"); got != http.StatusNoContent {
		t.Errorf("unmark 001 for now: %d", got)
	}
	// Only a completed scenario's page lists the missions it leads to, one link each.
	if p := body(t, srv, "/refusalwar/scenario_000/", cookie); !strings.Contains(p, `id="next-missions"`) || !strings.Contains(p, "<h2>Next mission</h2>") || !strings.Contains(p, `<li><a class="button" href="/refusalwar/scenario_001/">`) {
		t.Errorf("a completed scenario links to its next missions: %.300s", p)
	}
	if p := body(t, srv, "/refusalwar/scenario_001/", cookie); strings.Contains(p, `id="next-missions"`) {
		t.Error("an uncompleted scenario lists no next missions")
	}
	page = cards()
	if !strings.Contains(page, `<li class="completed"><a class="card completed" href="/refusalwar/scenario_000/">`) || !strings.Contains(page, "Completed &#10003;") || !strings.Contains(page, `<li class="available"><a class="card available" href="/refusalwar/scenario_001/">`) {
		t.Fatalf("000 is completed (green) and 001 is available: %.1500s", page)
	}
	if strings.Contains(page, "show-all-scenarios") {
		t.Error("with nothing locked there is nothing to show")
	}
	if p := body(t, srv, "/refusalwar/scenario_000/", cookie); strings.Contains(p, "Mark not completed") || !strings.Contains(p, `data-done="1"`) {
		t.Error("a completed outcome scenario cannot be unmarked")
	}
	if p := body(t, srv, "/refusalwar/scenario_000/", cookie); !strings.Contains(p, "001 – Pyre Light") {
		t.Error("now 001 is in the menu")
	}
	if got := complete("scenario_001", "1"); got != http.StatusNoContent {
		t.Errorf("complete 001: %d", got)
	}
	if p := body(t, srv, "/refusalwar/scenario_001/", cookie); !strings.Contains(p, "Mark not completed") {
		t.Error("a completed scenario without an outcome offers to be unmarked")
	}
	// Un-completing 001 leaves 000 completed (and 000, which has a committed outcome, cannot be un-completed).
	if got := complete("scenario_001", "0"); got != http.StatusNoContent {
		t.Errorf("unmark 001: %d", got)
	}
	if got := complete("scenario_000", "0"); got != http.StatusConflict {
		t.Errorf("unmark 000: %d", got)
	}
	done, _ := store.Completions(ctx, id, "refusalwar")
	if len(done) != 1 || !done["scenario_000"] {
		t.Errorf("only 000 is left completed: %v", done)
	}
	// Deleting the campaign deletes its log.
	if ok, err := store.DeleteRun(ctx, id, "refusalwar"); err != nil || !ok {
		t.Fatalf("delete: %v %v", ok, err)
	}
	if done, _ := store.Completions(ctx, id, "refusalwar"); len(done) != 0 {
		t.Errorf("a deleted campaign keeps no log: %v", done)
	}
}
