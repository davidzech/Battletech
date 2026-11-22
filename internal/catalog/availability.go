package catalog

import (
	_ "embed"
	"encoding/json"
	"sync"
)

//go:embed availability.json
var availabilityJSON []byte

var (
	availOnce sync.Once
	availSets map[string]map[string]bool
)

// Available is the set of unit names available to a faction in an era, or nil if the pair isn't known (so no filter can apply).
func Available(faction, era string) map[string]bool {
	availOnce.Do(func() {
		var raw map[string][]string
		if err := json.Unmarshal(availabilityJSON, &raw); err != nil {
			panic("availability.json: " + err.Error())
		}
		availSets = make(map[string]map[string]bool, len(raw))
		for k, names := range raw {
			set := make(map[string]bool, len(names))
			for _, n := range names {
				set[n] = true
			}
			availSets[k] = set
		}
	})
	return availSets[faction+"|"+era]
}
