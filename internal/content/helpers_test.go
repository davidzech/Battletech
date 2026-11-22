package content

import (
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
	sc.CompileOutcomes()
	if sc.OutcomeError != "" || !sc.HasOutcome() {
		t.Fatalf("an Aces-style scenario: %q", sc.OutcomeError)
	}
	return sc
}

func sps(res OutcomeResult) string {
	var out []string
	for _, p := range res.Pilots {
		out = append(out, p.Name+"="+strconv.Itoa(p.SP))
	}
	return strings.Join(out, ",")
}

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

func refusalWar(t *testing.T) *Campaign {
	t.Helper()
	c, ok := (Library{Root: "../../campaigns"}).Campaign("refusalwar")
	if !ok || c.StartingBV != 10000 || c.StartingMinMechs != 5 {
		t.Fatalf("Refusal War should start with at least 5 mechs and up to 10000 BV: %+v", c)
	}
	return c
}
