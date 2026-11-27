package content

import (
	"strings"
	"testing"
)

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
	good.CompileOutcomes()
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
		sc.CompileOutcomes()
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

// Scenario 001 follows the Aces model: its objectives and the Jade Falcon mechs put SP into the pool, each pilot draws a share, and the rest is the warchest.
func TestScenario001Outcome(t *testing.T) {
	sc, ok := refusalWar(t).Scenario("scenario_001")
	if !ok || !sc.HasOutcome() {
		t.Fatalf("Scenario 001 has an outcome engine: %+v", sc)
	}
	names := map[string]string{"a": "Alrek", "b": "Katarina"}
	res, err := sc.EvaluateOutcome(OutcomeInputs{
		Pilots:  []OutcomeEntry{{PilotID: "a", TookPart: true}, {PilotID: "b", TookPart: true, Killed: true}},
		Mission: map[string]any{"plant_large": true, "plant_small": true, "firebase_inspected": true, "objective_1": false, "objective_5": false, "falcon_mechs": float64(3)},
	}, names)
	if err != nil {
		t.Fatal(err)
	}
	// Pool: 400 + 250 + 450 + 3 x 200 = 1700. Alrek draws 100, Katarina (killed) nothing, so 1600 goes to the warchest.
	if res.Pool != 1700 || res.Pilots[0].SP != 100 || res.Pilots[1].SP != 0 || !res.Pilots[1].KIA || res.Warchest != 1600 {
		t.Errorf("pool %d, pilots %+v, warchest %d", res.Pool, res.Pilots, res.Warchest)
	}
	// The chemical plant completely destroyed (both buildings, which are required) grants Scorched Earth.
	if strings.Join(res.Keywords, ",") != "scorched_earth" {
		t.Errorf("keywords %v", res.Keywords)
	}
}

// The primary objectives are required: the mission cannot be completed without them, whatever the form says.
func TestScenario001PrimaryObjectivesAreRequired(t *testing.T) {
	sc, _ := refusalWar(t).Scenario("scenario_001")
	for _, missing := range []string{"plant_large", "plant_small"} {
		mission := map[string]any{"plant_large": true, "plant_small": true, "firebase_inspected": false, "objective_1": false, "objective_5": false, "falcon_mechs": float64(0)}
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

// The MVP earns an extra mvpBonusSP, on top of their share; only one pilot can be it, and only one who took part and lives.
func TestMVPBonus(t *testing.T) {
	sc := acesScenario(t, 80)
	sc.MVPSP = 20 // Refusal War's mvp_sp
	mission := map[string]any{"depot": true, "escape": false}
	run := func(entries ...OutcomeEntry) (OutcomeResult, error) {
		return sc.EvaluateOutcome(OutcomeInputs{Pilots: entries, Mission: mission}, map[string]string{"a": "Alrek", "b": "Katarina", "c": "Jarek", "d": "Sybille"})
	}
	res, err := run(OutcomeEntry{PilotID: "a", TookPart: true, MVP: true}, OutcomeEntry{PilotID: "b", TookPart: true})
	if err != nil || sps(res) != "Alrek=100,Katarina=80" || !res.Pilots[0].MVP || res.Pool != 400 || res.Warchest != 240 {
		t.Errorf("the MVP gets 20 on top of 80, and it does not come out of the pool: %+v %v (%s)", res, err, sps(res))
	}
	for name, entries := range map[string][]OutcomeEntry{
		"two":     {{PilotID: "a", TookPart: true, MVP: true}, {PilotID: "b", TookPart: true, MVP: true}},
		"benched": {{PilotID: "a", TookPart: false, MVP: true}},
		"killed":  {{PilotID: "a", TookPart: true, Killed: true, MVP: true}},
	} {
		if _, err := run(entries...); err == nil {
			t.Errorf("%s: an MVP who cannot be one is refused", name)
		}
	}
}

// A mission with no MVP bonus (a campaign without mvp_sp) refuses an MVP rather than silently giving nothing.
func TestNoMVPWithoutABonus(t *testing.T) {
	sc := acesScenario(t, 80)
	if _, err := sc.EvaluateOutcome(OutcomeInputs{Pilots: []OutcomeEntry{{PilotID: "a", TookPart: true, MVP: true}}, Mission: map[string]any{"depot": true, "escape": false}}, map[string]string{"a": "Alrek"}); err == nil {
		t.Error("an MVP in a mission without one is refused")
	}
	if sc, _ := refusalWar(t).Scenario("scenario_001"); sc.MVPSP != 20 {
		t.Errorf("Refusal War's missions take its mvp_sp: %d", sc.MVPSP)
	}
}

// Scenario 000 promotes: every cadet who passes the Trial becomes at least a MechWarrior, and the one with the most kills a Star Commander, the MVP breaking a
// tie. A promotion never lowers a rank, and a pilot the mission removes is not promoted.
func TestScenario000Promotions(t *testing.T) {
	c := refusalWar(t)
	sc, _ := c.Scenario("scenario_000")
	names := map[string]string{"a": "Alrek", "b": "Katarina", "c": "Jarek"}
	run := func(entries ...OutcomeEntry) map[string]PilotOutcome {
		t.Helper()
		res, err := sc.EvaluateOutcome(OutcomeInputs{Pilots: entries}, names)
		if err != nil {
			t.Fatal(err)
		}
		res.ResolvePromotions(map[string]string{"c": "Star Captain"}, c.Ranks) // Jarek already outranks both promotions
		out := map[string]PilotOutcome{}
		for _, p := range res.Pilots {
			out[p.PilotID] = p
		}
		return out
	}
	pilot := func(id string, kills int, killed, mvp bool) OutcomeEntry {
		return OutcomeEntry{PilotID: id, TookPart: true, Killed: killed, MVP: mvp, Values: map[string]any{"kills": float64(kills)}}
	}
	check := func(what string, got map[string]PilotOutcome, want map[string]string) {
		t.Helper()
		for id, rank := range want {
			if got[id].Rank != rank {
				t.Errorf("%s: %s takes rank %q, want %q (notes %v)", what, got[id].Name, got[id].Rank, rank, got[id].Notes)
			}
		}
	}
	check("most kills", run(pilot("a", 2, false, false), pilot("b", 1, false, true)), map[string]string{"a": "Star Commander", "b": "MechWarrior"})
	check("a tie goes to the MVP", run(pilot("a", 1, false, false), pilot("b", 1, false, true)), map[string]string{"a": "MechWarrior", "b": "Star Commander"})
	check("a tie with no MVP yet", run(pilot("a", 1, false, false), pilot("b", 1, false, false)), map[string]string{"a": "MechWarrior", "b": "MechWarrior"})
	// A killed pilot's kills don't count: the most kills among the survivors does.
	got := run(pilot("a", 3, true, false), pilot("b", 1, false, true))
	check("the dead don't compete", got, map[string]string{"a": "", "b": "Star Commander"})
	if !got["a"].KIA {
		t.Error("Alrek was killed")
	}
	// No kills: removed from the campaign (Merchant Caste), so not promoted.
	check("no kills", run(pilot("a", 0, false, false), pilot("b", 2, false, true)), map[string]string{"a": "", "b": "Star Commander"})
	// A promotion is "at least": Jarek, a Star Captain with the most kills, keeps his rank.
	got = run(pilot("c", 4, false, true), pilot("a", 1, false, false))
	check("never a demotion", got, map[string]string{"c": "", "a": "MechWarrior"})
	if p := got["a"]; p.Notes[len(p.Notes)-1] != "Promoted to MechWarrior." {
		t.Errorf("the report says so: %v", p.Notes)
	}
}

// A promotion to a rank the campaign doesn't have is caught when the campaign loads.
func TestPromotionToAnUnknownRank(t *testing.T) {
	c := refusalWar(t)
	sc, _ := c.Scenario("scenario_000")
	sc.OutcomeRules = append(sc.OutcomeRules, OutcomeRule{When: "kills > 3", Promote: "Grand Poobah"})
	sc.OutcomeError = ""
	c.checkPromotions()
	if !strings.Contains(sc.OutcomeError, `"Grand Poobah", which is not one of the campaign's ranks`) {
		t.Errorf("OutcomeError = %q", sc.OutcomeError)
	}
}

// Scenario 004: getting the repaired Mist Lynx out (1,000 SP and Counter Intelligence) replaces the primary's 600 SP; each Falcon mech crippled or destroyed adds
// 100. The aftermath follows which of the two got out.
func TestScenario004Outcome(t *testing.T) {
	sc, ok := refusalWar(t).Scenario("scenario_004")
	if !ok || !sc.HasOutcome() {
		t.Fatalf("Scenario 004 has an outcome engine: %+v", sc)
	}
	for _, tc := range []struct {
		core, lynx bool
		pool       int
		keywords   string
		aftermath  string
	}{
		{true, false, 600 + 200, "", "partially corrupted"},
		{false, true, 1000 + 200, "counter_intelligence", "The Keshik is pleased"},
		{true, true, 1000 + 200, "counter_intelligence", "The Keshik is pleased"},
		{false, false, 200, "", ""},
	} {
		res, err := sc.EvaluateOutcome(OutcomeInputs{
			Pilots:  []OutcomeEntry{{PilotID: "a", TookPart: true}},
			Mission: map[string]any{"data_core": tc.core, "mist_lynx": tc.lynx, "falcon_mechs": float64(2)},
		}, map[string]string{"a": "Alrek"})
		if err != nil {
			t.Fatal(err)
		}
		after := string(sc.AftermathFor(&res))
		if res.Pool != tc.pool || strings.Join(res.Keywords, ",") != tc.keywords || (tc.aftermath == "") != (after == "") || !strings.Contains(after, tc.aftermath) {
			t.Errorf("%+v: pool %d, keywords %v, aftermath %q", tc, res.Pool, res.Keywords, after)
		}
	}
}

// Scenario 005: each member of the staff rescued pays SP; all four rescued earn Savior, two or more killed Blackout, and the aftermath follows the count.
func TestScenario005Outcome(t *testing.T) {
	sc, ok := refusalWar(t).Scenario("scenario_005")
	if !ok || !sc.HasOutcome() {
		t.Fatalf("Scenario 005 has an outcome engine: %+v", sc)
	}
	for _, tc := range []struct {
		radio, tech, worker, cmdr bool
		pool                      int
		keywords, aftermath       string
	}{
		{true, true, true, true, 800 + 500, "savior", "Honorable deeds beget praise"},
		{true, true, false, true, 800 + 450, "", "will not hamper"},
		{true, false, false, true, 800 + 350, "blackout", "no survivors"},
		{false, false, false, false, 800, "blackout", "no survivors"},
	} {
		res, err := sc.EvaluateOutcome(OutcomeInputs{
			Pilots:  []OutcomeEntry{{PilotID: "a", TookPart: true}},
			Mission: map[string]any{"defenders_destroyed": true, "radio_operative": tc.radio, "technician": tc.tech, "worker": tc.worker, "array_commander": tc.cmdr},
		}, map[string]string{"a": "Alrek"})
		if err != nil {
			t.Fatal(err)
		}
		after := string(sc.AftermathFor(&res))
		if res.Pool != tc.pool || strings.Join(res.Keywords, ",") != tc.keywords || !strings.Contains(after, tc.aftermath) || strings.Count(after, "The sensor array dome is secure")+strings.Count(after, "integrity was compromised") != 1 {
			t.Errorf("%+v: pool %d, keywords %v, aftermath %q", tc, res.Pool, res.Keywords, after)
		}
	}
}

// A pilot removed by a kia rule draws no pilot_sp share, like one reported killed: as if never deployed, so their share stays in the pool for the warchest.
func TestKIARuleDrawsNoShare(t *testing.T) {
	sc := &Scenario{
		OutcomePilotSP:    100,
		OutcomeObjectives: []OutcomeObjective{{ID: "depot", Label: "Destroy the depot", SP: 400}, {ID: "escape", Label: "Escape with the data", SP: 200}},
		OutcomeInputs:     []OutcomeInput{{ID: "ejected", Type: "bool"}},
		OutcomeRules:      []OutcomeRule{{When: "ejected", KIA: true}},
	}
	sc.CompileOutcomes()
	if sc.OutcomeError != "" {
		t.Fatal(sc.OutcomeError)
	}
	res, err := sc.EvaluateOutcome(OutcomeInputs{
		Pilots: []OutcomeEntry{
			{PilotID: "a", TookPart: true, Values: map[string]any{"ejected": false}},
			{PilotID: "b", TookPart: true, Values: map[string]any{"ejected": true}},
		},
		Mission: map[string]any{"depot": true, "escape": false},
	}, map[string]string{"a": "Alrek", "b": "Katarina"})
	if err != nil || !res.Pilots[1].KIA || sps(res) != "Alrek=100,Katarina=0" || res.Pool != 400 || res.Paid != 100 || res.Warchest != 300 {
		t.Errorf("only the surviving pilot draws from the pool: %+v %v (%s)", res, err, sps(res))
	}
}
