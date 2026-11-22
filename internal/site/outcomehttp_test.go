package site

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/pilot"
	"github.com/davidzech/Battletech/internal/storage"
)

func scenario000(t *testing.T) *content.Scenario {
	t.Helper()
	sc, ok := refusalWar(t).Scenario("scenario_000")
	if !ok || !sc.HasOutcome() {
		t.Fatalf("Scenario 000 should have an outcome engine: %+v", sc)
	}
	return sc
}

func entry(id string, took bool, kills int, killed bool) content.OutcomeEntry {
	return content.OutcomeEntry{PilotID: id, TookPart: took, Killed: killed, Values: map[string]any{"kills": float64(kills)}}
}

func TestMissionOutcomeEndToEnd(t *testing.T) {
	srv, store, cookie := startedCompany(t) // Alrek and Katarina, 150 SP each, warchest 5,000
	ctx := context.Background()
	id := playthroughID("wolfpack2026")
	pilots, _ := store.Pilots(ctx, id, "refusalwar")
	pid := map[string]string{}
	for _, p := range pilots {
		pid[p.Name] = p.ID
	}
	form := func(scenario string, entries ...content.OutcomeEntry) url.Values {
		b, _ := json.Marshal(content.OutcomeInputs{Pilots: entries})
		return url.Values{"campaign": {"refusalwar"}, "scenario": {scenario}, "inputs": {string(b)}}
	}
	preview := func(v url.Values, origin string) *http.Response {
		return post(t, srv, "/api/campaign/scenario-outcome", v, cookie, origin)
	}
	commit := func(v url.Values) int {
		return post(t, srv, "/api/campaign/scenario-outcome/commit", v, cookie, srv.URL).StatusCode
	}
	state := func() (alrek, katarina pilot.NamedPilot, run storage.CampaignRun) {
		ps, _ := store.Pilots(ctx, id, "refusalwar")
		for _, p := range ps {
			if p.Name == "Alrek" {
				alrek = p
			}
			if p.Name == "Katarina" {
				katarina = p
			}
		}
		run, _, _ = store.Run(ctx, id, "refusalwar")
		return
	}
	a0, k0, run0 := state()

	// The mission page offers the completion dialog instead of a plain mark.
	page := body(t, srv, "/refusalwar/scenario_000/", cookie)
	if !strings.Contains(page, `id="outcome-open"`) || !strings.Contains(page, `id="outcome-dialog"`) || strings.Contains(page, "mission-log-toggle") || !strings.Contains(page, "/static/outcome.js") || !strings.Contains(page, "Alrek") {
		t.Fatalf("Scenario 000 completes by reporting its outcome: %.500s", page)
	}
	// The plain mark and a scenario without an outcome engine are refused.
	if got := post(t, srv, "/api/campaign/scenario-complete", url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_000"}, "done": {"1"}}, cookie, srv.URL).StatusCode; got != http.StatusBadRequest {
		t.Errorf("marking an outcome scenario by hand: %d", got)
	}
	if got := preview(form("scenario_001", entry(pid["Alrek"], true, 1, false)), srv.URL).StatusCode; got != http.StatusBadRequest {
		t.Errorf("a scenario with no outcome: %d", got)
	}
	// Bad requests change nothing.
	if got := preview(form("scenario_000", entry(pid["Alrek"], true, 2, false)), "https://evil.example").StatusCode; got != http.StatusForbidden {
		t.Errorf("cross-origin: %d", got)
	}
	for name, v := range map[string]url.Values{
		"too many kills":  form("scenario_000", entry(pid["Alrek"], true, 9, false)),
		"a stranger":      form("scenario_000", entry("nobody", true, 1, false)),
		"unreadable form": {"campaign": {"refusalwar"}, "scenario": {"scenario_000"}, "inputs": {"{nope"}},
	} {
		if got := commit(v); got != http.StatusBadRequest {
			t.Errorf("%s: %d", name, got)
		}
	}

	// The preview works the result out and changes nothing.
	r := preview(form("scenario_000", entry(pid["Alrek"], true, 2, false), entry(pid["Katarina"], true, 0, false)), srv.URL)
	var res content.OutcomeResult
	if r.StatusCode != http.StatusOK || json.NewDecoder(r.Body).Decode(&res) != nil || len(res.Pilots) != 2 || res.Pilots[0].SP != 400 || !res.Pilots[1].KIA || res.FreeRecruits != 1 {
		t.Fatalf("preview: %d %+v", r.StatusCode, res)
	}
	if a, k, run := state(); a.TotalSP != a0.TotalSP || k.Fallen || run.FreeRecruits != 0 {
		t.Fatal("a preview must not change anything")
	}
	if done, _ := store.Completions(ctx, id, "refusalwar"); len(done) != 0 {
		t.Fatalf("nothing is completed yet: %v", done)
	}

	// Alrek is the only pilot who took part and stays, so he is the only possible MVP, and the mission can't be committed without him chosen.
	if strings.Join(res.MVPCandidates, ",") != pid["Alrek"] {
		t.Errorf("the MVP candidates: %v", res.MVPCandidates)
	}
	if got := commit(form("scenario_000", entry(pid["Alrek"], true, 2, false), entry(pid["Katarina"], true, 0, false))); got != http.StatusBadRequest {
		t.Fatalf("a commit without the MVP: %d", got)
	}
	katarinaMVP := entry(pid["Katarina"], true, 0, false)
	katarinaMVP.MVP = true
	if got := commit(form("scenario_000", entry(pid["Alrek"], true, 2, false), katarinaMVP)); got != http.StatusBadRequest {
		t.Fatalf("a commit with an MVP who fails the Trial: %d", got)
	}
	// Committing applies it all: SP for the survivor (and the MVP's 20), the other pilot killed in action, a free recruit, and the mission completed.
	alrekMVP := entry(pid["Alrek"], true, 2, false)
	alrekMVP.MVP = true
	if got := commit(form("scenario_000", alrekMVP, entry(pid["Katarina"], true, 0, false))); got != http.StatusNoContent {
		t.Fatalf("commit: %d", got)
	}
	a1, k1, run1 := state()
	if a1.TotalSP != a0.TotalSP+420 || a1.Fallen || a1.SP != a0.SP {
		t.Errorf("Alrek earns 400 SP and 20 as the MVP, unallocated: %+v", a1)
	}
	if !k1.Fallen || k1.TotalSP != k0.TotalSP {
		t.Errorf("Katarina is killed in action: %+v", k1)
	}
	if run1.FreeRecruits != 1 || run1.Warchest != run0.Warchest {
		t.Errorf("a free recruit and an untouched warchest: %+v", run1)
	}
	if done, _ := store.Completions(ctx, id, "refusalwar"); !done["scenario_000"] {
		t.Fatal("Scenario 000 is completed")
	}
	// It can only happen once, and cannot be undone here.
	if got := commit(form("scenario_000", entry(pid["Alrek"], true, 3, false))); got != http.StatusConflict {
		t.Errorf("committing twice: %d", got)
	}
	if got := preview(form("scenario_000", entry(pid["Alrek"], true, 3, false)), srv.URL).StatusCode; got != http.StatusConflict {
		t.Errorf("previewing a completed mission: %d", got)
	}
	if got := post(t, srv, "/api/campaign/scenario-complete", url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_000"}, "done": {"0"}}, cookie, srv.URL).StatusCode; got != http.StatusConflict {
		t.Errorf("un-completing a committed outcome: %d", got)
	}
	if a, _, _ := state(); a.TotalSP != a0.TotalSP+420 {
		t.Errorf("the rewards were given once: %d", a.TotalSP)
	}
	// The page now reports what happened, 001 is open, and the Complete button is gone.
	page = body(t, srv, "/refusalwar/scenario_000/", cookie)
	for _, want := range []string{`id="outcome-summary"`, "<b>Alrek</b>", `<span class="outcome-mvp-badge">MVP</span>`, "Kills: 2", "&#43;400 SP", "&#43;20 SP (MVP)", "<b>Katarina</b>", "permanently removed", "Completed &#10003;"} {
		if !strings.Contains(page, want) {
			t.Errorf("the report on the page is missing %q", want)
		}
	}
	if strings.Contains(page, `id="outcome-open"`) || strings.Contains(page, `id="outcome-dialog"`) {
		t.Error("a completed mission has nothing left to report")
	}
	if !strings.Contains(body(t, srv, "/refusalwar/", cookie), `class="card available" href="/refusalwar/scenario_001/"`) {
		t.Error("Scenario 001 is available now")
	}

	// The replacement pilot is free: the note says so, and recruiting uses the credit instead of the warchest.
	man := body(t, srv, "/refusalwar/manage/", cookie)
	if !strings.Contains(man, "A replacement pilot is free: recruiting costs no SP.") || !strings.Contains(man, `data-cost="0"`) || strings.Contains(man, `id="recruit-open" disabled`) {
		t.Errorf("a free replacement: %.300s", man)
	}
	if got := post(t, srv, "/api/pilots", url.Values{"campaign": {"refusalwar"}, "name": {"Newcomer"}, "gunnery": {"90"}, "edge_tokens": {"60"}}, cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("recruit the replacement: %d", got)
	}
	if _, _, run := state(); run.FreeRecruits != 0 || run.Warchest != run0.Warchest {
		t.Errorf("the free recruit was used, not the warchest: %+v", run)
	}
	if got := post(t, srv, "/api/pilots", url.Values{"campaign": {"refusalwar"}, "name": {"Another"}}, cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("the next recruit: %d", got)
	}
	if _, _, run := state(); run.Warchest != run0.Warchest-300 {
		t.Errorf("the next one costs 300 SP again: %d", run.Warchest)
	}
}

// A refused recruit gives the free recruit back.
func TestFreeRecruitIsRestoredOnFailure(t *testing.T) {
	srv, store, cookie := startedCompany(t)
	ctx := context.Background()
	id := playthroughID("wolfpack2026")
	if _, err := store.DB().Exec(`UPDATE campaign_runs SET free_recruits = 1`); err != nil {
		t.Fatal(err)
	}
	if got := post(t, srv, "/api/pilots", url.Values{"campaign": {"refusalwar"}, "name": {"alrek"}}, cookie, srv.URL).StatusCode; got != http.StatusBadRequest {
		t.Fatalf("a taken name: %d", got)
	}
	if run, _, _ := store.Run(ctx, id, "refusalwar"); run.FreeRecruits != 1 || run.Warchest != 5000 {
		t.Errorf("the free recruit and the warchest are untouched: %+v", run)
	}
	if ok, _ := store.ConsumeFreeRecruit(ctx, id, "refusalwar"); !ok {
		t.Error("consume")
	}
	if ok, _ := store.ConsumeFreeRecruit(ctx, id, "refusalwar"); ok {
		t.Error("none left")
	}
}

// The Trial of Position has an MVP like every Refusal War mission: the campaign's mvp_sp (20) on top of the kill reward. A pilot the rules remove (no kills) can't be
// it, any more than one killed in action, and the completion dialog is told the bonus so it can offer the pick.
func TestScenario000MVP(t *testing.T) {
	sc := scenario000(t)
	if sc.MVPSP != 20 {
		t.Fatalf("Scenario 000 takes Refusal War's mvp_sp: %d", sc.MVPSP)
	}
	names := map[string]string{"a": "Alrek", "b": "Katarina"}
	mvp := func(e content.OutcomeEntry) content.OutcomeEntry { e.MVP = true; return e }
	res, err := sc.EvaluateOutcome(content.OutcomeInputs{Pilots: []content.OutcomeEntry{mvp(entry("a", true, 2, false)), entry("b", true, 1, false)}}, names)
	if err != nil || res.Pilots[0].SP != 420 || !res.Pilots[0].MVP || res.Pilots[1].SP != 150 {
		t.Errorf("the MVP earns 400 + 20: %+v %v", res, err)
	}
	// A pilot who fails the Trial leaves the campaign, so they can't be the MVP: the preview drops them as the MVP (and the commit refuses it), and with them gone
	// only Katarina can be it.
	res, err = sc.EvaluateOutcome(content.OutcomeInputs{Pilots: []content.OutcomeEntry{mvp(entry("a", true, 0, false)), entry("b", true, 1, false)}}, names)
	if err != nil || res.MVPRefused != "Alrek" || res.Pilots[0].MVP || res.Pilots[0].SP != 0 || strings.Join(res.MVPCandidates, ",") != "b" || !res.MVPMissing() {
		t.Errorf("a pilot who fails the Trial cannot be the MVP: %+v %v", res, err)
	}
	// Nobody who took part stays (both fail): no MVP can be chosen, and none is needed.
	res, err = sc.EvaluateOutcome(content.OutcomeInputs{Pilots: []content.OutcomeEntry{entry("a", true, 0, false), entry("b", true, 0, true)}}, names)
	if err != nil || len(res.MVPCandidates) != 0 || res.MVPMissing() {
		t.Errorf("no candidates, no MVP needed: %+v %v", res, err)
	}

	srv, _, cookie := startedCompany(t)
	if page := body(t, srv, "/refusalwar/scenario_000/", cookie); !strings.Contains(page, `data-mvp-sp="20"`) {
		t.Error("the completion dialog should be told the MVP bonus")
	}
}
