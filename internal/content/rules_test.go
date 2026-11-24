package content

import (
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/mekbay"
)

// Scenario 000's own front matter lists the four mechs, and every provided unit of every scenario is a real mech.
func TestScenario000ProvidesItsOwnMechs(t *testing.T) {
	lib := Library{Root: "../../campaigns"}
	c, _ := lib.Campaign("refusalwar")
	sc, ok := c.Scenario("scenario_000")
	if !ok || strings.Join(sc.ProvidedUnits, ",") != strings.Join(scenario000Pool, ",") {
		t.Fatalf("Scenario 000 should provide Nova, Stormcrow, Shadow Cat, Pouncer and Mongrel (Grendel) Primes: %+v", sc.ProvidedUnits)
	}
	if sc1, _ := c.Scenario("scenario_001"); sc1.Provides() {
		t.Error("Scenario 001 uses the player's hangar")
	}
	camps, err := lib.Campaigns()
	if err != nil {
		t.Fatal(err)
	}
	for _, camp := range camps {
		for _, s := range camp.Scenarios {
			for _, name := range s.ProvidedUnits {
				if u, ok := catalog.ByName(name); !ok || u.Type != "Mek" {
					t.Errorf("%s provides %q, which is not a mech in the catalog", s.Slug, name)
				}
			}
		}
	}
}

// Scenario 000 allows two mechs in all, each with a named pilot, and has no BV limit.
func TestScenarioRules(t *testing.T) {
	sc := &Scenario{UnitLimit: 2, PilotsRequired: true}
	crew := func(units ...string) []mekbay.CrewAssignment {
		var out []mekbay.CrewAssignment
		for _, u := range units {
			out = append(out, mekbay.CrewAssignment{UnitID: u, Name: "Alrek"})
		}
		return out
	}
	for name, c := range map[string]struct {
		units [][]string
		crews [][]mekbay.CrewAssignment
		bad   bool
	}{
		"two mechs, both crewed":      {[][]string{{"a", "b"}}, [][]mekbay.CrewAssignment{crew("a", "b")}, false},
		"one each for two players":    {[][]string{{"a"}, {"b"}}, [][]mekbay.CrewAssignment{crew("a"), crew("b")}, false},
		"three mechs is over":         {[][]string{{"a", "b"}, {"c"}}, [][]mekbay.CrewAssignment{crew("a", "b"), crew("c")}, true},
		"a mech without a pilot":      {[][]string{{"a", "b"}}, [][]mekbay.CrewAssignment{crew("a")}, true},
		"a pilot sent empty":          {[][]string{{"a"}}, [][]mekbay.CrewAssignment{{{UnitID: "a"}}}, true},
		"no assignments at all":       {[][]string{{"a"}}, nil, true},
		"another player's pilot only": {[][]string{{"a"}, {"b"}}, [][]mekbay.CrewAssignment{crew("b"), crew("b")}, true},
	} {
		if msg := sc.RulesProblem(c.units, c.crews); (msg != "") != c.bad {
			t.Errorf("%s: %q", name, msg)
		}
	}
	// No rules, no problems.
	if msg := (&Scenario{}).RulesProblem([][]string{{"a", "b", "c"}}, nil); msg != "" {
		t.Errorf("a scenario without rules: %q", msg)
	}
}
