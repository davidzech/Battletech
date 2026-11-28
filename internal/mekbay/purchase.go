package mekbay

import (
	"strings"
	"time"
)

// HangarGroup is the name of the group a hangar force's units sit in, where a purchase adds new units.
const HangarGroup = "Hangar"

// SameGroupName compares group names the way the site reads them: ignoring case and surrounding space.
func SameGroupName(name, want string) bool {
	return strings.EqualFold(strings.TrimSpace(name), want)
}

// AddUnitsToForce adds a fresh, undamaged unit for each ID to the "Hangar" group of a saved force (the decoded JSON of its `data`), creating the
// group if it doesn't exist. Everything else in the force is left as it was. Only a version 2 force can fail (a unit with no UUID in the catalog).
func AddUnitsToForce(force map[string]any, unitIDs []string, now time.Time) error {
	if ForceVersion(force) == 2 {
		return v2AddUnits(force, unitIDs)
	}
	groups, _ := force["groups"].([]any)
	var hangar map[string]any
	for _, g := range groups {
		if gm, ok := g.(map[string]any); ok {
			if name, _ := gm["name"].(string); hangar == nil && SameGroupName(name, HangarGroup) {
				hangar = gm
			}
		}
	}
	if hangar == nil {
		hangar = map[string]any{"id": NewID(), "name": HangarGroup, "color": "", "units": []any{}}
		groups = append(groups, hangar)
		force["groups"] = groups
	}
	units, _ := hangar["units"].([]any)
	for _, id := range unitIDs {
		units = append(units, map[string]any{
			"id": NewID(), "unit": id, "alias": "", "updatedTs": now.UnixMilli(),
			"state": map[string]any{
				"crew":      unnamedCrew(),
				"crits":     []any{},
				"heat":      map[string]any{"current": 0, "previous": 0},
				"locations": map[string]any{},
				"modified":  false,
				"destroyed": false,
				"turnState": map[string]any{"applyMovePSR": false},
			},
		})
	}
	hangar["units"] = units
	return nil
}
