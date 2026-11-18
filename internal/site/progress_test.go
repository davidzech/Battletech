package site

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// A scratch campaign: 000 lists its branches (skipping 001), 001 leaves them out (so leads to 002), 002 names a default_next, 003 is the end of the line.
func scratchCampaign(t *testing.T) *Campaign {
	t.Helper()
	root := t.TempDir()
	write := func(dir, file, text string) {
		p := filepath.Join(root, "Scratch", dir)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, file), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []string{"000", "001", "002", "003", "004"} {
		write("Scenario_"+n, "Setup.md", "---\nid: scenario_"+n+"\ntitle: Mission "+n+"\n---\n# Mission\n")
	}
	write("Scenario_000", "Outcome.md", "---\nid: scenario_000\nbranches: [scenario_002, Scenario_003]\n---\n")
	write("Scenario_001", "Outcome.md", "---\nid: scenario_001\n---\n")
	write("Scenario_002", "Outcome.md", "---\nid: scenario_002\ndefault_next: scenario_004\n---\n")
	write("Scenario_003", "Outcome.md", "---\nid: scenario_003\nbranches: []\n---\n")
	c, ok := (Library{Root: root}).Campaign("scratch")
	if !ok {
		t.Fatal("scratch campaign not loaded")
	}
	return c
}

func TestScenarioBranches(t *testing.T) {
	c := scratchCampaign(t)
	branches := func(slug string) string {
		sc, _ := c.Scenario(slug)
		return strings.Join(sc.Branches, ",")
	}
	for slug, want := range map[string]string{
		"scenario_000": "scenario_002,scenario_003", // listed, lowercased
		"scenario_001": "scenario_002",              // omitted: the next number
		"scenario_002": "scenario_004",              // omitted but with a default_next
		"scenario_003": "",                          // an empty list ends the path
		"scenario_004": "",                          // omitted, but there is no scenario 005
	} {
		if got := branches(slug); got != want {
			t.Errorf("%s branches = %q, want %q", slug, got, want)
		}
	}
	set := func(m map[string]bool) string {
		var out []string
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}
	for name, c2 := range map[string]struct {
		completed []string
		want      string
	}{
		"nothing done: only the first":     {nil, "scenario_000"},
		"000 done: its branches open":      {[]string{"scenario_000"}, "scenario_000,scenario_002,scenario_003"},
		"001 is not reachable from 000":    {[]string{"scenario_000", "scenario_002"}, "scenario_000,scenario_002,scenario_003,scenario_004"},
		"a completed one is always shown":  {[]string{"scenario_001"}, "scenario_000,scenario_001,scenario_002"},
		"an unknown completion is ignored": {[]string{"scenario_999"}, "scenario_000"},
	} {
		done := map[string]bool{}
		for _, s := range c2.completed {
			done[s] = true
		}
		if got := set(c.Available(done)); got != c2.want {
			t.Errorf("%s: %s, want %s", name, got, c2.want)
		}
	}
	// Refusal War: 000 leads to 001, and 001 is the end for now.
	rw := refusalWar(t)
	if s0, _ := rw.Scenario("scenario_000"); strings.Join(s0.Branches, ",") != "scenario_001" {
		t.Errorf("Scenario 000 leads to 001: %v", s0.Branches)
	}
	if s1, _ := rw.Scenario("scenario_001"); len(s1.Branches) != 0 {
		t.Errorf("Scenario 001 leads nowhere yet: %v", s1.Branches)
	}
	for _, camp := range []*Campaign{rw, c} {
		for _, sc := range camp.Scenarios {
			for _, b := range sc.Branches {
				if _, ok := camp.Scenario(b); !ok {
					t.Errorf("%s branches to %q, which is not a scenario", sc.Slug, b)
				}
			}
		}
	}
}

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
