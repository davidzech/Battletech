package site

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func scenario000(t *testing.T) *Scenario {
	t.Helper()
	sc, ok := refusalWar(t).Scenario("scenario_000")
	if !ok || !sc.HasOutcome() {
		t.Fatalf("Scenario 000 should have an outcome engine: %+v", sc)
	}
	return sc
}

func entry(id string, took bool, kills int, killed bool) OutcomeEntry {
	return OutcomeEntry{PilotID: id, TookPart: took, Killed: killed, Values: map[string]any{"kills": float64(kills)}}
}

func TestScenario000Outcomes(t *testing.T) {
	sc := scenario000(t)
	names := map[string]string{"a": "Alrek", "b": "Katarina", "c": "Jarek"}
	run := func(entries ...OutcomeEntry) (OutcomeResult, error) {
		return sc.EvaluateOutcome(OutcomeInputs{Pilots: entries}, names)
	}
	// Kills 1 to 4 pay 150, 400, 650 and 900 SP to a pilot who lives.
	for kills, sp := range map[int]int{1: 150, 2: 400, 3: 650, 4: 900} {
		res, err := run(entry("a", true, kills, false))
		if err != nil || len(res.Pilots) != 1 || res.Pilots[0].SP != sp || res.Pilots[0].KIA || res.FreeRecruits != 0 {
			t.Errorf("%d kills: %+v %v (want %d SP)", kills, res, err, sp)
		}
	}
	// No kills, or killed in action: removed from the campaign, with a free replacement and no SP, however many kills.
	for name, e := range map[string]OutcomeEntry{"no kills": entry("a", true, 0, false), "killed": entry("a", true, 2, true), "killed with none": entry("a", true, 0, true)} {
		res, err := run(e)
		if err != nil || res.Pilots[0].SP != 0 || !res.Pilots[0].KIA || res.FreeRecruits != 1 {
			t.Errorf("%s: %+v %v", name, res, err)
		}
	}
	// Two pilots are judged separately, and one who did not take part is left out entirely.
	res, err := run(entry("a", true, 3, false), entry("b", true, 0, false), entry("c", false, 0, false))
	if err != nil || len(res.Pilots) != 3 || res.Pilots[0].SP != 650 || !res.Pilots[1].KIA || res.FreeRecruits != 1 || res.Pilots[2].Status != "benched" || res.Pilots[2].SP != 0 || res.Pilots[2].KIA {
		t.Fatalf("two pilots: %+v %v", res, err)
	}
	if res.Pilots[0].Name != "Alrek" || res.Pilots[0].Entered[0] != "Kills: 3" || res.Pilots[0].Notes[0] != "+650 SP" {
		t.Errorf("the report says what was entered and what follows: %+v", res.Pilots[0])
	}
	if len(res.Pilots[1].Notes) != 1 || res.Pilots[1].Notes[0] != "Failed Trial of Position with 0 kills. Demoted to Merchant Caste." {
		t.Errorf("a pilot with no kills fails the Trial: %v", res.Pilots[1].Notes)
	}
	// A pilot killed in action reads simply "Killed in Action.", whatever their kills (even none), and each removed pilot earns a free recruit.
	for _, kills := range []int{0, 2} {
		res, err := run(entry("a", true, kills, true), entry("b", true, 0, false))
		if err != nil || len(res.Pilots[0].Notes) != 1 || res.Pilots[0].Notes[0] != "Killed in Action." || !res.Pilots[0].KIA || res.Pilots[0].SP != 0 || res.FreeRecruits != 2 {
			t.Errorf("killed with %d kills: %+v %v", kills, res, err)
		}
	}
	// A benched pilot is listed but nothing happens to them (Scenario 000 has no pilot SP to share), and nothing else changes.
	if res, err := run(entry("a", false, 0, false)); err != nil || len(res.Pilots) != 1 || res.Pilots[0].Status != "benched" || res.Pilots[0].Notes[0] != "Benched: no rewards, no losses." || res.FreeRecruits != 0 || res.Warchest != 0 {
		t.Errorf("nobody took part: %+v %v", res, err)
	}
	// Bad reports are refused.
	for name, in := range map[string]OutcomeInputs{
		"too many kills":         {Pilots: []OutcomeEntry{entry("a", true, 5, false)}},
		"negative kills":         {Pilots: []OutcomeEntry{entry("a", true, -1, false)}},
		"half a kill":            {Pilots: []OutcomeEntry{{PilotID: "a", TookPart: true, Values: map[string]any{"kills": 1.5}}}},
		"a word for kills":       {Pilots: []OutcomeEntry{{PilotID: "a", TookPart: true, Values: map[string]any{"kills": "two"}}}},
		"an unknown input":       {Pilots: []OutcomeEntry{{PilotID: "a", TookPart: true, Values: map[string]any{"kills": 1.0, "bogus": 1.0}}}},
		"an unknown pilot":       {Pilots: []OutcomeEntry{entry("zz", true, 1, false)}},
		"the same pilot":         {Pilots: []OutcomeEntry{entry("a", true, 1, false), entry("a", true, 2, false)}},
		"a mission input":        {Pilots: []OutcomeEntry{entry("a", true, 1, false)}, Mission: map[string]any{"bonus": 1.0}},
		"a benched pilot killed": {Pilots: []OutcomeEntry{{PilotID: "a", TookPart: false, Killed: true}}},
		"killed as a count":      {Pilots: []OutcomeEntry{{PilotID: "a", TookPart: true, Values: map[string]any{"kills": 1.0, "killed": 2.0}}}},
	} {
		if _, err := sc.EvaluateOutcome(in, names); err == nil {
			t.Errorf("%s should be refused", name)
		}
	}
	// A pilot who didn't take part is not validated (nothing of theirs is used).
	if _, err := run(OutcomeEntry{PilotID: "a", TookPart: false, Values: map[string]any{"kills": 99.0}}); err != nil {
		t.Errorf("an absent pilot's values are ignored: %v", err)
	}
}

func TestOutcomeRulesCompile(t *testing.T) {
	good := &Scenario{
		OutcomeInputs: []OutcomeInput{{ID: "kills"}, {ID: "won", Type: "bool", Scope: "mission"}, {ID: "salvage", Scope: "mission", Max: 500}},
		OutcomeRules: []OutcomeRule{
			{When: "kills >= 1", SP: "kills * 100"},
			{When: "won", Warchest: "salvage * 2", Label: "Victory salvage"},
			{When: "won and salvage > 0", Warchest: 50},
			{KIA: true, When: "kills == 0 and not won"},
		},
	}
	good.compileOutcomes()
	if good.OutcomeError != "" || !good.HasOutcome() {
		t.Fatalf("a good scenario: %q", good.OutcomeError)
	}
	// Mission rules read mission inputs only and are tried once; pilot rules are tried per pilot.
	if good.OutcomeRules[0].pilot != true || good.OutcomeRules[1].pilot || good.OutcomeRules[2].pilot || !good.OutcomeRules[3].pilot {
		t.Errorf("rule scopes: %v %v %v %v", good.OutcomeRules[0].pilot, good.OutcomeRules[1].pilot, good.OutcomeRules[2].pilot, good.OutcomeRules[3].pilot)
	}
	res, err := good.EvaluateOutcome(OutcomeInputs{
		Pilots:  []OutcomeEntry{{PilotID: "a", TookPart: true, Values: map[string]any{"kills": 2.0}}, {PilotID: "b", TookPart: true, Values: map[string]any{"kills": 1.0}}},
		Mission: map[string]any{"won": true, "salvage": 100.0},
	}, map[string]string{"a": "A", "b": "B"})
	if err != nil || res.Pilots[0].SP != 200 || res.Pilots[1].SP != 100 || res.Warchest != 250 || len(res.Notes) != 1 || res.Notes[0] != "Victory salvage" {
		t.Errorf("a mission with a mission input: %+v %v", res, err)
	}
	for name, sc := range map[string]*Scenario{
		"an input with no id":      {OutcomeInputs: []OutcomeInput{{Label: "x"}}, OutcomeRules: []OutcomeRule{{SP: 1}}},
		"a repeated input":         {OutcomeInputs: []OutcomeInput{{ID: "a"}, {ID: "a"}}, OutcomeRules: []OutcomeRule{{SP: 1}}},
		"a bad scope":              {OutcomeInputs: []OutcomeInput{{ID: "a", Scope: "squad"}}, OutcomeRules: []OutcomeRule{{SP: 1}}},
		"a bad type":               {OutcomeInputs: []OutcomeInput{{ID: "a", Type: "text"}}, OutcomeRules: []OutcomeRule{{SP: 1}}},
		"min above max":            {OutcomeInputs: []OutcomeInput{{ID: "a", Min: 5, Max: 2}}, OutcomeRules: []OutcomeRule{{SP: 1}}},
		"a bad condition":          {OutcomeInputs: []OutcomeInput{{ID: "a"}}, OutcomeRules: []OutcomeRule{{When: "a ==", SP: 1}}},
		"a bad amount":             {OutcomeInputs: []OutcomeInput{{ID: "a"}}, OutcomeRules: []OutcomeRule{{SP: "a *"}}},
		"a name that is no input":  {OutcomeInputs: []OutcomeInput{{ID: "a"}}, OutcomeRules: []OutcomeRule{{When: "b > 1", SP: 1}}},
		"a name in a warchest sum": {OutcomeInputs: []OutcomeInput{{ID: "a"}}, OutcomeRules: []OutcomeRule{{Warchest: "zzz"}}},
	} {
		sc.compileOutcomes()
		if sc.OutcomeError == "" || sc.HasOutcome() {
			t.Errorf("%s should be a front matter error", name)
		}
	}
	// Every scenario of every campaign compiles.
	camps, _ := (Library{Root: "../../campaigns"}).Campaigns()
	for _, c := range camps {
		for _, sc := range c.Scenarios {
			if sc.OutcomeError != "" {
				t.Errorf("%s: %s", sc.Slug, sc.OutcomeError)
			}
		}
	}
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
	form := func(scenario string, entries ...OutcomeEntry) url.Values {
		b, _ := json.Marshal(OutcomeInputs{Pilots: entries})
		return url.Values{"campaign": {"refusalwar"}, "scenario": {scenario}, "inputs": {string(b)}}
	}
	preview := func(v url.Values, origin string) *http.Response {
		return post(t, srv, "/api/campaign/scenario-outcome", v, cookie, origin)
	}
	commit := func(v url.Values) int {
		return post(t, srv, "/api/campaign/scenario-outcome/commit", v, cookie, srv.URL).StatusCode
	}
	state := func() (alrek, katarina NamedPilot, run CampaignRun) {
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
	var res OutcomeResult
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
	if _, err := store.db.Exec(`UPDATE campaign_runs SET free_recruits = 1`); err != nil {
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

// Scenario 001 follows the Aces model: its objectives and the Jade Falcon mechs put SP into the pool, each pilot draws a share, and the rest is the warchest.
func TestScenario001Outcome(t *testing.T) {
	sc, ok := refusalWar(t).Scenario("scenario_001")
	if !ok || !sc.HasOutcome() {
		t.Fatalf("Scenario 001 has an outcome engine: %+v", sc)
	}
	names := map[string]string{"a": "Alrek", "b": "Katarina"}
	res, err := sc.EvaluateOutcome(OutcomeInputs{
		Pilots:  []OutcomeEntry{{PilotID: "a", TookPart: true}, {PilotID: "b", TookPart: true, Killed: true}},
		Mission: map[string]any{"plant_large": true, "plant_small": true, "firebase_inspected": true, "waypoint_a": false, "waypoint_e": false, "falcon_mechs": float64(3)},
	}, names)
	if err != nil {
		t.Fatal(err)
	}
	// Pool: 400 + 250 + 450 + 3 x 200 = 1700. Alrek draws 100, Katarina (killed) nothing, so 1600 goes to the warchest.
	if res.Pool != 1700 || res.Pilots[0].SP != 100 || res.Pilots[1].SP != 0 || !res.Pilots[1].KIA || res.Warchest != 1600 {
		t.Errorf("pool %d, pilots %+v, warchest %d", res.Pool, res.Pilots, res.Warchest)
	}
}

// The primary objectives are required: the mission cannot be completed without them, whatever the form says.
func TestScenario001PrimaryObjectivesAreRequired(t *testing.T) {
	sc, _ := refusalWar(t).Scenario("scenario_001")
	for _, missing := range []string{"plant_large", "plant_small"} {
		mission := map[string]any{"plant_large": true, "plant_small": true, "firebase_inspected": false, "waypoint_a": false, "waypoint_e": false, "falcon_mechs": float64(0)}
		mission[missing] = false
		_, err := sc.EvaluateOutcome(OutcomeInputs{Pilots: []OutcomeEntry{{PilotID: "a", TookPart: true}}, Mission: mission}, map[string]string{"a": "Alrek"})
		if err == nil || !strings.Contains(err.Error(), "required") {
			t.Errorf("without %s: %v", missing, err)
		}
	}
	var required []string
	for _, in := range sc.OutcomeInputs {
		if in.Required {
			required = append(required, in.ID)
		}
	}
	if strings.Join(required, ",") != "plant_large,plant_small" {
		t.Errorf("the required inputs are sent to the dialog: %v", required)
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
	mvp := func(e OutcomeEntry) OutcomeEntry { e.MVP = true; return e }
	res, err := sc.EvaluateOutcome(OutcomeInputs{Pilots: []OutcomeEntry{mvp(entry("a", true, 2, false)), entry("b", true, 1, false)}}, names)
	if err != nil || res.Pilots[0].SP != 420 || !res.Pilots[0].MVP || res.Pilots[1].SP != 150 {
		t.Errorf("the MVP earns 400 + 20: %+v %v", res, err)
	}
	// A pilot who fails the Trial leaves the campaign, so they can't be the MVP: the preview drops them as the MVP (and the commit refuses it), and with them gone
	// only Katarina can be it.
	res, err = sc.EvaluateOutcome(OutcomeInputs{Pilots: []OutcomeEntry{mvp(entry("a", true, 0, false)), entry("b", true, 1, false)}}, names)
	if err != nil || res.MVPRefused != "Alrek" || res.Pilots[0].MVP || res.Pilots[0].SP != 0 || strings.Join(res.MVPCandidates, ",") != "b" || !res.MVPMissing() {
		t.Errorf("a pilot who fails the Trial cannot be the MVP: %+v %v", res, err)
	}
	// Nobody who took part stays (both fail): no MVP can be chosen, and none is needed.
	res, err = sc.EvaluateOutcome(OutcomeInputs{Pilots: []OutcomeEntry{entry("a", true, 0, false), entry("b", true, 0, true)}}, names)
	if err != nil || len(res.MVPCandidates) != 0 || res.MVPMissing() {
		t.Errorf("no candidates, no MVP needed: %+v %v", res, err)
	}

	srv, _, cookie := startedCompany(t)
	if page := body(t, srv, "/refusalwar/scenario_000/", cookie); !strings.Contains(page, `data-mvp-sp="20"`) {
		t.Error("the completion dialog should be told the MVP bonus")
	}
}
