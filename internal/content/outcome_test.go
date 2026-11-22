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
