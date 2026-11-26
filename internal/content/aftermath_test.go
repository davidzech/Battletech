package content

import (
	"encoding/json"
	"strings"
	"testing"
)

// A scenario's aftermath follows its recorded report: Scenario 003's transmission depends on what the inspection found and what was done about it, and Scenario
// 001's war story is shown whatever happened. The sections are taken out of the Outcome fold.
func TestAftermath(t *testing.T) {
	c := refusalWar(t)
	sc, _ := c.Scenario("scenario_003")
	report := func(mission map[string]any) *OutcomeResult {
		// The way the page gets it: recorded as JSON, so the numbers come back as floats.
		b, _ := json.Marshal(OutcomeResult{Mission: mission})
		var res OutcomeResult
		if err := json.Unmarshal(b, &res); err != nil {
			t.Fatal(err)
		}
		return &res
	}
	for _, tc := range []struct {
		karyoplasma, destroyed, scanned bool
		want                            string
	}{
		{true, true, false, "inspected and destroyed"},
		{true, false, false, "disgraced the Clan"},
		{false, false, true, "data files of the destroyed Star Commander"},
		{false, false, false, "intelligence network has failed us"},
	} {
		got := string(sc.AftermathFor(report(map[string]any{"converter_inspected": true, "karyoplasma": tc.karyoplasma, "converter_destroyed": tc.destroyed, "commander_scanned": tc.scanned})))
		if !strings.Contains(got, tc.want) || strings.Count(got, "<p>") != 2 {
			t.Errorf("%+v: the one matching transmission, got %s", tc, got)
		}
	}
	if got := sc.AftermathFor(nil); got != "" {
		t.Errorf("without a report no condition can be judged, and 003 has nothing else: %q", got)
	}
	if strings.Contains(string(sc.OutcomeHTML), "Keshik") {
		t.Error("the aftermath is not repeated in the Outcome")
	}

	one, _ := c.Scenario("scenario_001")
	plain := string(one.AftermathFor(nil))
	if !strings.Contains(plain, "set the tone for the entire Refusal War") || strings.Contains(plain, "The Keshik is pleased") {
		t.Errorf("Scenario 001 completed without a report shows only its war story: %s", plain)
	}
	full := string(one.AftermathFor(report(map[string]any{"plant_large": true, "plant_small": true, "firebase_inspected": false, "objective_1": false, "objective_5": false, "falcon_mechs": 2})))
	if !strings.HasPrefix(full, "<p>Your strike has left the chemical plant in ruins") || !strings.Contains(full, "set the tone for the entire Refusal War") {
		t.Errorf("with the report, the transmission leads: %s", full)
	}
	if !strings.Contains(string(one.NextStepsHTML), "Flame Tongue") || !strings.Contains(string(one.NextStepsHTML), "Blade Splint") || strings.Contains(string(one.OutcomeHTML), "Next Steps") {
		t.Errorf("Scenario 001's Next Steps describe its choice, outside the Outcome: %s", one.NextStepsHTML)
	}
}

// An aftermath's condition is checked like an outcome rule's, against the mission inputs only.
func TestAftermathProblems(t *testing.T) {
	for _, tc := range []struct {
		aftermath []AftermathText
		want      string
	}{
		{[]AftermathText{{When: "depot"}}, "has no text"},
		{[]AftermathText{{When: "depoot", Text: "x"}}, "when"},
		{[]AftermathText{{When: "kills > 1", Text: "x"}}, "when"},
	} {
		sc := acesScenario(t, 80)
		sc.OutcomeInputs = append(sc.OutcomeInputs, OutcomeInput{ID: "kills", Scope: "pilot", Type: "number"})
		sc.Aftermath = tc.aftermath
		if err := sc.compileAftermath(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: %v", tc.aftermath, err)
		}
	}
	plain := &Scenario{Aftermath: []AftermathText{{When: "x", Text: "y"}}}
	if err := plain.compileAftermath(); err == nil {
		t.Error("a condition needs the outcome engine")
	}
}
