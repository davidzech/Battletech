package site

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/storage"
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

	// A locked mission, previewed, shows what it leads to: 001's choice of 002 or 003, locked too, on a muted bracket.
	if p := body(t, srv, "/refusalwar/scenario_001/", cookie); !strings.Contains(p, `<section class="next-missions preview" id="next-missions">`) || !strings.Contains(p, "<h2>Leads to</h2>") ||
		!strings.Contains(p, `<a class="card locked" href="/refusalwar/scenario_002/">`) || !strings.Contains(p, `<a class="card locked" href="/refusalwar/scenario_003/">`) ||
		strings.Count(p, `<span class="go">Locked</span>`) != 2 || strings.Count(p, `<path class="sg-passed" d="M8 50H24"`) != 2 {
		t.Errorf("a locked mission previews its branches: %.300s", p)
	}

	// Before the account is named or the campaign started, only Scenario 000 shows, and Show all reveals the rest.
	page := cards()
	if !strings.Contains(page, `<a class="card available" href="/refusalwar/scenario_000/">`) || !strings.Contains(page, `<a class="card locked" href="/refusalwar/scenario_001/">`) {
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
	if p := body(t, srv, "/refusalwar/scenario_001/", cookie); !strings.Contains(p, "<span>Not available</span>") || strings.Contains(p, "mission-log-toggle") {
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
	if p := body(t, srv, "/refusalwar/scenario_000/", cookie); !strings.Contains(p, `id="next-missions"`) || !strings.Contains(p, "<h2>Next mission</h2>") || !strings.Contains(p, `<a class="card available" href="/refusalwar/scenario_001/"><span class="num">001</span>`) {
		t.Errorf("a completed scenario links to its next missions: %.300s", p)
	}
	if p := body(t, srv, "/refusalwar/scenario_001/", cookie); strings.Contains(p, `id="next-missions"`) {
		t.Error("an uncompleted scenario lists no next missions")
	}
	page = cards()
	if !strings.Contains(page, `<a class="card completed" href="/refusalwar/scenario_000/">`) || !strings.Contains(page, "Completed") || !strings.Contains(page, `<a class="card available" href="/refusalwar/scenario_001/">`) {
		t.Fatalf("000 is completed (green) and 001 is available: %.1500s", page)
	}
	if !strings.Contains(page, "show-all-scenarios") {
		t.Error("002 is locked until 001 is completed, so it can be shown")
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
	// 001 offers a choice of 002 or 003; once 002 is completed, 003 (the path not taken) closes and 001 no longer links to it.
	if p := body(t, srv, "/refusalwar/scenario_001/", cookie); !strings.Contains(p, `href="/refusalwar/scenario_002/"`) || !strings.Contains(p, `<a class="card available" href="/refusalwar/scenario_003/">`) {
		t.Error("001 offers both 002 and 003")
	}
	// The choice is a bracket: a dotted line from the first card to the last (nothing above the first or below the last), each option on a stub.
	if p := body(t, srv, "/refusalwar/scenario_001/", cookie); strings.Count(p, `<i class="sg-node sg-next" style="--lane-n: 1">`) != 2 || strings.Count(p, `<path class="sg-next" d="M8 50H24"`) != 2 ||
		strings.Count(p, `<path class="sg-next" d="M8 0V50"`) != 1 || strings.Count(p, `<path class="sg-next" d="M8 50V100"`) != 1 {
		t.Error("001's next missions are drawn as a bracket")
	}
	if _, err := store.SetScenarioCompleted(ctx, id, "refusalwar", "scenario_002", true); err != nil {
		t.Fatal(err)
	}
	if p := body(t, srv, "/refusalwar/scenario_003/", cookie); !strings.Contains(p, "<span>Not available</span>") {
		t.Error("after 002, 003 is locked")
	}
	if p := cards(); !strings.Contains(p, `<li class="locked passed"><span class="sgraph">`) || !strings.Contains(p, `<a class="card locked" href="/refusalwar/scenario_003/">`) {
		t.Error("003, the choice not taken, stays listed (locked) for the path graph")
	}
	if p := cards(); !strings.Contains(p, `href="/refusalwar/scenario_003/"><span class="num">003</span><span class="name">Blade Splint<span class="sub">Zoetermeer</span></span><span class="go">Not available</span>`) {
		t.Error("a mission passed over reads Not available, not Locked")
	}
	if p := body(t, srv, "/refusalwar/scenario_001/", cookie); strings.Contains(p, `href="/refusalwar/scenario_003/"><span class="num">`) || !strings.Contains(p, `<a class="card completed" href="/refusalwar/scenario_002/">`) {
		t.Error("001 no longer links to the branch not taken, and the one taken shows completed")
	}
	if _, err := store.SetScenarioCompleted(ctx, id, "refusalwar", "scenario_002", false); err != nil {
		t.Fatal(err)
	}
	if p := body(t, srv, "/refusalwar/scenario_003/", cookie); strings.Contains(p, "<span>Not available</span>") {
		t.Error("un-completing 002 reopens the choice")
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

// Repeating a completion toggle changes nothing: no second history line, no second checkpoint or hangar copy, and un-marking a mission that was never completed
// is not logged.
func TestRepeatedCompletionChangesNothing(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	id := playthroughID("wolfpack2026")
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	if got := post(t, srv, "/api/campaign/scenario-outcome/commit", url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_000"}, "inputs": {`{"pilots":[],"mission":{}}`}}, cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("complete 000: %d", got)
	}
	mark := func(scenario, done string) {
		t.Helper()
		if got := post(t, srv, "/api/campaign/scenario-complete", url.Values{"campaign": {"refusalwar"}, "scenario": {scenario}, "done": {done}}, cookie, srv.URL).StatusCode; got != http.StatusNoContent {
			t.Fatalf("mark %s done=%s: %d", scenario, done, got)
		}
	}
	// counts are the history lines, the checkpoints and the hangar copies made so far.
	counts := func() [3]int {
		t.Helper()
		h, err := store.History(ctx, id, "refusalwar")
		if err != nil {
			t.Fatal(err)
		}
		cps, err := store.Checkpoints(ctx, id, "refusalwar")
		if err != nil {
			t.Fatal(err)
		}
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return [3]int{len(h), len(cps), len(fake.copies)}
	}

	base := counts()
	mark("scenario_001", "1")
	once := counts()
	if once != [3]int{base[0] + 1, base[1] + 1, base[2] + 1} {
		t.Fatalf("completing logs one line and takes one checkpoint with one hangar copy: %v after %v", once, base)
	}
	mark("scenario_001", "1")
	if got := counts(); got != once {
		t.Errorf("completing again changes nothing (lines, checkpoints, copies): %v, was %v", got, once)
	}
	mark("scenario_001", "0")
	undone := counts()
	if undone != [3]int{once[0] + 1, once[1], once[2]} {
		t.Fatalf("un-marking logs one line and no checkpoint: %v after %v", undone, once)
	}
	mark("scenario_001", "0")
	mark("scenario_002", "0") // never completed
	if got := counts(); got != undone {
		t.Errorf("un-marking what is not completed changes nothing: %v, was %v", got, undone)
	}

	// The store says whether anything changed, and completing forgets the battle whether or not it did.
	if changed, err := store.SetScenarioCompleted(ctx, id, "refusalwar", "scenario_001", true); err != nil || !changed {
		t.Fatalf("a first completion is a change: %v %v", changed, err)
	}
	if err := store.SaveMekBayOperation(ctx, id, "refusalwar", "scenario_001", storage.MekBayOperation{OpForInstance: "of", OperationID: "op"}); err != nil {
		t.Fatal(err)
	}
	if changed, err := store.SetScenarioCompleted(ctx, id, "refusalwar", "scenario_001", true); err != nil || changed {
		t.Errorf("a second completion is not: %v %v", changed, err)
	}
	if _, had, _ := store.MekBayOperation(ctx, id, "refusalwar", "scenario_001"); had {
		t.Error("a completed mission has no battle in progress")
	}
	if changed, err := store.SetScenarioCompleted(ctx, id, "refusalwar", "scenario_003", false); err != nil || changed {
		t.Errorf("un-marking what was never completed is not a change: %v %v", changed, err)
	}
}
