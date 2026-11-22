package content

import (
	"fmt"

	"github.com/davidzech/Battletech/internal/mekbay"
)

// RulesProblem checks a plan against the scenario's own rules, beyond the BV limit: the most mechs in all (unit_limit), and a named pilot on every mech
// (pilots_required). units[i] and crews[i] are player i+1's mech IDs and pilot assignments. It returns the first problem as a sentence, or "".
func (sc *Scenario) RulesProblem(units [][]string, crews [][]mekbay.CrewAssignment) string {
	total := 0
	for _, u := range units {
		total += len(u)
	}
	if sc.UnitLimit > 0 && total > sc.UnitLimit {
		return fmt.Sprintf("This mission allows at most %d mechs in all; you chose %d.", sc.UnitLimit, total)
	}
	if sc.PilotsRequired {
		for i, u := range units {
			named := map[string]bool{}
			if i < len(crews) {
				for _, a := range crews[i] {
					if a.Name != "" {
						named[a.UnitID] = true
					}
				}
			}
			for _, id := range u {
				if !named[id] {
					return "Every mech on this mission needs a named pilot: assign one to each."
				}
			}
		}
	}
	return ""
}

// Provides reports whether the scenario flies its own mechs in place of the hangar.
func (sc *Scenario) Provides() bool { return sc != nil && len(sc.ProvidedUnits) > 0 }
