package site

import (
	"testing"

	"github.com/davidzech/Battletech/internal/content"
)

// The MVP earns an extra mvpBonusSP, on top of their share; only one pilot can be it, and only one who took part and lives.
func TestMVPBonus(t *testing.T) {
	sc := acesScenario(t, 80)
	sc.MVPSP = 20 // Refusal War's mvp_sp
	mission := map[string]any{"depot": true, "escape": false}
	run := func(entries ...content.OutcomeEntry) (content.OutcomeResult, error) {
		return sc.EvaluateOutcome(content.OutcomeInputs{Pilots: entries, Mission: mission}, map[string]string{"a": "Alrek", "b": "Katarina", "c": "Jarek", "d": "Sybille"})
	}
	res, err := run(content.OutcomeEntry{PilotID: "a", TookPart: true, MVP: true}, content.OutcomeEntry{PilotID: "b", TookPart: true})
	if err != nil || sps(res) != "Alrek=100,Katarina=80" || !res.Pilots[0].MVP || res.Pool != 400 || res.Warchest != 240 {
		t.Errorf("the MVP gets 20 on top of 80, and it does not come out of the pool: %+v %v (%s)", res, err, sps(res))
	}
	for name, entries := range map[string][]content.OutcomeEntry{
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
	if _, err := sc.EvaluateOutcome(content.OutcomeInputs{Pilots: []content.OutcomeEntry{{PilotID: "a", TookPart: true, MVP: true}}, Mission: map[string]any{"depot": true, "escape": false}}, map[string]string{"a": "Alrek"}); err == nil {
		t.Error("an MVP in a mission without one is refused")
	}
	if sc, _ := refusalWar(t).Scenario("scenario_001"); sc.MVPSP != 20 {
		t.Errorf("Refusal War's missions take its mvp_sp: %d", sc.MVPSP)
	}
}
