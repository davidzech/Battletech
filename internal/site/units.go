package site

import (
	_ "embed"
	"encoding/json"
	"sync"
)

//go:generate go run gen_units.go

// unitsIndexJSON is the slice of MekBay's unit catalog the site needs (see gen_units.go). The full
// data/units.json is not shipped in the Docker image, so this is embedded instead.
//
//go:embed units_index.json
var unitsIndexJSON []byte

// Unit is one catalog entry: Name is MekBay's unit ID (e.g. BMMadCat_H), which is what a saved force
// stores in each unit's `unit` field.
type Unit struct {
	Name    string `json:"name"`
	Chassis string `json:"chassis"`
	Model   string `json:"model"`
	BV      int    `json:"bv"`
}

var (
	unitsOnce sync.Once
	unitsByCM map[[2]string]Unit
)

// lookupUnit finds a unit by the chassis and model strings a MegaMek/MekBay .mul file uses,
// e.g. ("Mad Cat (Timber Wolf)", "H") or ("Grizzly", "").
func lookupUnit(chassis, model string) (Unit, bool) {
	unitsOnce.Do(func() {
		var all []Unit
		if err := json.Unmarshal(unitsIndexJSON, &all); err != nil {
			panic("units_index.json: " + err.Error())
		}
		unitsByCM = make(map[[2]string]Unit, len(all))
		for _, u := range all {
			k := [2]string{u.Chassis, u.Model}
			if _, dup := unitsByCM[k]; !dup {
				unitsByCM[k] = u
			}
		}
	})
	u, ok := unitsByCM[[2]string{chassis, model}]
	return u, ok
}
