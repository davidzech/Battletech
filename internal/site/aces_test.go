package site

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The BattleTech: Aces model: objectives met fill a pool; each active pilot takes a flat share of it, a benched pilot half, a killed pilot none; the rest of the pool
// goes to the warchest. Keywords are granted by objectives met or missed.
func acesScenario(t *testing.T, pilotSP any) *Scenario {
	t.Helper()
	sc := &Scenario{
		OutcomePilotSP: pilotSP,
		OutcomeObjectives: []OutcomeObjective{
			{ID: "depot", Label: "Destroy the depot", SP: 400, Grants: []string{"depot_gone"}, UnmetGrants: []string{"depot_standing"}},
			{ID: "escape", Label: "Escape with the data", SP: 200, Grants: []string{"data_saved"}},
		},
	}
	sc.compileOutcomes()
	if sc.OutcomeError != "" || !sc.HasOutcome() {
		t.Fatalf("an Aces-style scenario: %q", sc.OutcomeError)
	}
	return sc
}

func acesRun(t *testing.T, sc *Scenario, mission map[string]any, entries ...OutcomeEntry) OutcomeResult {
	t.Helper()
	res, err := sc.EvaluateOutcome(OutcomeInputs{Pilots: entries, Mission: mission}, map[string]string{"a": "Alrek", "b": "Katarina", "c": "Jarek", "d": "Sybille"})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func pilotEntry(id string, active, killed bool) OutcomeEntry {
	return OutcomeEntry{PilotID: id, TookPart: active, Killed: killed}
}

func sps(res OutcomeResult) string {
	var out []string
	for _, p := range res.Pilots {
		out = append(out, p.Name+"="+strconv.Itoa(p.SP))
	}
	return strings.Join(out, ",")
}

func TestAcesOutcomeModel(t *testing.T) {
	sc := acesScenario(t, 80)
	// The example: both objectives met is 600 SP. Four pilots, two active: (80 + 80) + (40 + 40) = 240 from the pool, 360 to the warchest.
	res := acesRun(t, sc, map[string]any{"depot": true, "escape": true},
		pilotEntry("a", true, false), pilotEntry("b", true, false), pilotEntry("c", false, false), pilotEntry("d", false, false))
	if res.Pool != 600 || sps(res) != "Alrek=80,Katarina=80,Jarek=40,Sybille=40" || res.Warchest != 360 || res.FreeRecruits != 0 {
		t.Fatalf("600 SP pool, 80 each: %+v (%s)", res, sps(res))
	}
	if res.Pilots[2].Status != "benched" || res.Pilots[0].Status != "active" || res.Pilots[0].KIA || res.Pilots[2].Notes[0] != "+40 SP (benched: half)" || res.Pilots[0].Notes[0] != "+80 SP" {
		t.Errorf("statuses and notes: %+v", res.Pilots)
	}
	if res.Notes[len(res.Notes)-1] != "Pool: 600 SP. 240 to the pilots, 360 to the warchest." {
		t.Errorf("the report says where the pool went: %v", res.Notes)
	}
	// A pilot killed gets nothing and does not draw from the pool: 80 + 0 + 40 + 40 = 160, and 440 are left.
	res = acesRun(t, sc, map[string]any{"depot": true, "escape": true},
		pilotEntry("a", true, false), pilotEntry("b", true, true), pilotEntry("c", false, false), pilotEntry("d", false, false))
	if sps(res) != "Alrek=80,Katarina=0,Jarek=40,Sybille=40" || res.Warchest != 440 || !res.Pilots[1].KIA || res.Pilots[1].Status != "killed" || res.Pilots[0].KIA {
		t.Errorf("a pilot killed: %+v (%s)", res, sps(res))
	}
	// Only one objective met: a 400 SP pool.
	res = acesRun(t, sc, map[string]any{"depot": true}, pilotEntry("a", true, false), pilotEntry("b", true, false))
	if res.Pool != 400 || sps(res) != "Alrek=80,Katarina=80" || res.Warchest != 240 {
		t.Errorf("one objective: %+v", res)
	}
	// No objectives met: an empty pool, so nobody can be paid and nothing goes to the warchest.
	res = acesRun(t, sc, map[string]any{}, pilotEntry("a", true, false), pilotEntry("c", false, false))
	if res.Pool != 0 || sps(res) != "Alrek=0,Jarek=0" || res.Warchest != 0 {
		t.Errorf("an empty pool: %+v", res)
	}
	// A pool too small for every share is split in proportion: 100 SP between shares of 80, 80, 40 and 40 (240 due).
	small := &Scenario{OutcomePilotSP: 80, OutcomeObjectives: []OutcomeObjective{{ID: "x", SP: 100}}}
	small.compileOutcomes()
	res = acesRun(t, small, map[string]any{"x": true}, pilotEntry("a", true, false), pilotEntry("b", true, false), pilotEntry("c", false, false), pilotEntry("d", false, false))
	if sps(res) != "Alrek=33,Katarina=33,Jarek=16,Sybille=16" || res.Warchest != 2 {
		t.Errorf("a small pool is shared in proportion: %s warchest %d", sps(res), res.Warchest)
	}
	// A benched pilot's half of an odd share rounds down.
	odd := acesScenario(t, 81)
	if res := acesRun(t, odd, map[string]any{"depot": true, "escape": true}, pilotEntry("a", true, false), pilotEntry("c", false, false)); sps(res) != "Alrek=81,Jarek=40" {
		t.Errorf("half of 81 rounds down: %s", sps(res))
	}
	// A pilot share can be an expression over the mission inputs, and a pool without pilot_sp all goes to the warchest.
	byObjective := &Scenario{OutcomePilotSP: "depot ? 100 : 50", OutcomeObjectives: []OutcomeObjective{{ID: "depot", SP: 500}, {ID: "side", SP: 100}}}
	byObjective.compileOutcomes()
	if byObjective.OutcomeError != "" {
		t.Fatal(byObjective.OutcomeError)
	}
	if res := acesRun(t, byObjective, map[string]any{"depot": true, "side": true}, pilotEntry("a", true, false)); sps(res) != "Alrek=100" || res.Warchest != 500 {
		t.Errorf("an expression share: %+v", res)
	}
	noShare := &Scenario{OutcomeObjectives: []OutcomeObjective{{ID: "x", SP: 300}}}
	noShare.compileOutcomes()
	if res := acesRun(t, noShare, map[string]any{"x": true}, pilotEntry("a", true, false)); res.Warchest != 300 || sps(res) != "Alrek=0" {
		t.Errorf("no pilot SP: the pool is the warchest's: %+v", res)
	}
}

func TestObjectivesGrantKeywords(t *testing.T) {
	sc := acesScenario(t, 80)
	res := acesRun(t, sc, map[string]any{"depot": true, "escape": false}, pilotEntry("a", true, false))
	if strings.Join(res.Keywords, ",") != "depot_gone" {
		t.Errorf("objective met: %v", res.Keywords)
	}
	res = acesRun(t, sc, map[string]any{"depot": false, "escape": true}, pilotEntry("a", true, false))
	if strings.Join(res.Keywords, ",") != "depot_standing,data_saved" {
		t.Errorf("an unmet objective grants its own keyword: %v", res.Keywords)
	}
	// The Aces objectives are yes/no mission inputs with their SP as help.
	var depot OutcomeInput
	for _, in := range sc.OutcomeInputs {
		if in.ID == "depot" {
			depot = in
		}
	}
	if depot.Scope != "mission" || depot.Type != "bool" || depot.Help != "400 SP" || depot.Label != "Destroy the depot" {
		t.Errorf("objective input: %+v", depot)
	}
	// Mistakes: pool and grants belong to the mission, killed is built in, pilot_sp reads mission inputs only.
	for name, bad := range map[string]*Scenario{
		"a duplicate objective":          {OutcomeObjectives: []OutcomeObjective{{ID: "a"}, {ID: "a"}}},
		"killed is built in":             {OutcomeInputs: []OutcomeInput{{ID: "killed", Type: "bool"}}, OutcomeRules: []OutcomeRule{{SP: 1}}},
		"a pool in a pilot rule":         {OutcomeInputs: []OutcomeInput{{ID: "kills"}}, OutcomeRules: []OutcomeRule{{When: "kills > 1", SP: 5, Pool: 5}}},
		"grants in a pilot rule":         {OutcomeInputs: []OutcomeInput{{ID: "kills"}}, OutcomeRules: []OutcomeRule{{When: "kills > 1", Grants: []string{"x"}}}},
		"a bad keyword":                  {OutcomeObjectives: []OutcomeObjective{{ID: "a", Grants: []string{"Not A Keyword"}}}},
		"pilot_sp reading a pilot input": {OutcomePilotSP: "kills * 10", OutcomeInputs: []OutcomeInput{{ID: "kills"}}, OutcomeRules: []OutcomeRule{{SP: 1}}},
		"a bad pilot_sp":                 {OutcomePilotSP: "80 +", OutcomeObjectives: []OutcomeObjective{{ID: "a", SP: 1}}},
	} {
		bad.compileOutcomes()
		if bad.OutcomeError == "" {
			t.Errorf("%s should be a front matter error", name)
		}
	}
}

// Keywords.md is the registry: a scenario can only grant keywords listed there.
func TestKeywordRegistry(t *testing.T) {
	if got := strings.Join(keywordIDs("# Keywords\n\n- `depot_gone` - the depot was destroyed\n* `data_saved`: saved\n  - `nested_one` - indented\nnot a bullet `x_y`\n- `Bad_Case` no\n"), ","); got != "depot_gone,data_saved,nested_one" {
		t.Errorf("keyword IDs: %q", got)
	}
	root := t.TempDir()
	write := func(path, text string) {
		p := filepath.Join(root, "Scratch", path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Keywords.md", "# Keywords\n\n- `depot_gone` - the depot was destroyed\n")
	write("Scenario_000/Setup.md", "---\nid: scenario_000\ntitle: Good\n---\n# Good\n")
	write("Scenario_000/Outcome.md", "---\nid: scenario_000\npilot_sp: 50\nobjectives:\n  - id: depot\n    sp: 300\n    grants: [depot_gone]\n---\n")
	write("Scenario_001/Setup.md", "---\nid: scenario_001\ntitle: Bad\n---\n# Bad\n")
	write("Scenario_001/Outcome.md", "---\nid: scenario_001\nobjectives:\n  - id: depot\n    sp: 300\n    grants: [made_up_keyword]\n---\n")
	c, ok := (Library{Root: root}).Campaign("scratch")
	if !ok || strings.Join(c.Keywords, ",") != "depot_gone" {
		t.Fatalf("the registry: %+v %v", c, ok)
	}
	good, _ := c.Scenario("scenario_000")
	bad, _ := c.Scenario("scenario_001")
	if !good.HasOutcome() || good.OutcomeError != "" {
		t.Errorf("a registered keyword is fine: %q", good.OutcomeError)
	}
	if bad.HasOutcome() || !strings.Contains(bad.OutcomeError, "made_up_keyword") {
		t.Errorf("an unregistered keyword is refused: %q", bad.OutcomeError)
	}
}

// Committing an outcome puts its keywords in the campaign ledger (once each), shown on the keywords page.
func TestCampaignLedger(t *testing.T) {
	srv, store, cookie := startedCompany(t)
	ctx := context.Background()
	id := playthroughID("wolfpack2026")
	if p := body(t, srv, "/refusalwar/keywords/", cookie); !strings.Contains(p, "No keywords earned yet.") {
		t.Fatal("an empty ledger says so")
	}
	if err := store.CompleteWithOutcome(ctx, id, "refusalwar", "scenario_000", OutcomeResult{Pilots: []PilotOutcome{}, Keywords: []string{"depot_gone", "data_saved"}}); err != nil {
		t.Fatal(err)
	}
	ledger, _ := store.Ledger(ctx, id, "refusalwar")
	if len(ledger) != 2 || ledger[0].Scenario != "scenario_000" {
		t.Fatalf("ledger: %+v", ledger)
	}
	// A keyword already held is not recorded twice (a later scenario granting it again leaves the ledger alone).
	if err := store.CompleteWithOutcome(ctx, id, "refusalwar", "scenario_001", OutcomeResult{Pilots: []PilotOutcome{}, Keywords: []string{"depot_gone"}}); err != nil {
		t.Fatal(err)
	}
	if ledger, _ = store.Ledger(ctx, id, "refusalwar"); len(ledger) != 2 {
		t.Errorf("a keyword is held once: %+v", ledger)
	}
	if p := body(t, srv, "/refusalwar/keywords/", cookie); !strings.Contains(p, `id="ledger"`) || !strings.Contains(p, "<code>depot_gone</code>") || !strings.Contains(p, "<code>data_saved</code>") || strings.Contains(p, "No keywords earned yet.") {
		t.Errorf("the keywords page lists the ledger: %.600s", p)
	}
	if _, err := store.DeleteRun(ctx, id, "refusalwar"); err != nil {
		t.Fatal(err)
	}
	if ledger, _ = store.Ledger(ctx, id, "refusalwar"); len(ledger) != 0 {
		t.Errorf("a deleted campaign keeps no ledger: %+v", ledger)
	}
}
