package content

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/davidzech/Battletech/internal/catalog"
)

// A scenario's allies (Setup.md front matter `allies`: .mul files beside it) are units on the players' side that no player brings or controls, such as Temper
// Edge's downed Mist Lynx and the Savior sent to repair it. They cost nothing and aren't part of any player's force: Start battle builds them into a force of
// their own and adds it to the operation after the players' forces, on the friendly side.

// AlliedUnit is one of a scenario's allied units, as the page lists it.
type AlliedUnit struct {
	Name     string // the catalog's display name, e.g. "Mist Lynx C"
	Pilot    string
	Gunnery  int
	Piloting int
}

// loadAllies reads the allies' .mul files and returns what is wrong with them.
func (sc *Scenario) loadAllies() []string {
	var problems []string
	for _, f := range sc.Allies {
		if f == "" || f != filepath.Base(f) {
			problems = append(problems, fmt.Sprintf("allies: %q must be a file in the scenario's folder", f))
			continue
		}
		raw, err := os.ReadFile(filepath.Join(sc.Dir, f))
		if err != nil {
			problems = append(problems, fmt.Sprintf("allies: %s can't be read: %v", f, err))
			continue
		}
		entities, err := ParseMul(raw)
		if err != nil {
			problems = append(problems, fmt.Sprintf("allies: %s: %v", f, err))
			continue
		}
		for _, e := range entities {
			u, ok := catalog.Lookup(e.Chassis, e.Model)
			if !ok {
				problems = append(problems, fmt.Sprintf("allies: %s: %s %s is not in the unit catalog", f, e.Chassis, e.Model))
				continue
			}
			sc.AlliedUnits = append(sc.AlliedUnits, AlliedUnit{Name: catalog.DisplayName(u), Pilot: e.Pilot, Gunnery: e.Gunnery, Piloting: e.Piloting})
		}
	}
	return problems
}
