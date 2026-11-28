package mekbay

import (
	"errors"
	"time"
)

// SwapUnitState reconfigures the unit with this ID inside a force in MekBay's save format: its design becomes newName and its state is reset (a fresh unit has no
// damage, no critical hits and full ammunition, and the old variant's slots mean nothing to the new one). A unit that already is that design is ErrNoChange.
func SwapUnitState(force map[string]any, unitID, newName string, now time.Time) error {
	if ForceVersion(force) == 2 {
		return v2Swap(force, unitID, newName)
	}
	groups, _ := force["groups"].([]any)
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		units, _ := gm["units"].([]any)
		for _, u := range units {
			um, _ := u.(map[string]any)
			if um == nil || um["id"] != unitID {
				continue
			}
			if um["unit"] == newName {
				return ErrNoChange
			}
			pristineUnit(um, now)
			state, _ := um["state"].(map[string]any)
			state["crits"] = []any{}
			um["unit"] = newName
			return nil
		}
	}
	return errors.New("that unit is no longer in your hangar force")
}
