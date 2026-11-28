package mekbay

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Repair levels, lowest first.
const (
	RepairArmor     = "armor"
	RepairStructure = "structure"
	RepairCrippled  = "crippled"
	RepairDestroyed = "destroyed"
)

// ErrNoChange is what an edit to a unit answers when the hangar leaves it nothing to do: the repair, rearm or reconfiguration asked for has already been made (by
// a second click, or by another player at the table). The site saves nothing and charges nothing.
var ErrNoChange = errors.New("That has already been done to the unit (it may have changed). Reload and check your hangar.")

// ServiceUnitState applies a repair or rearm to the unit with this ID inside a force in MekBay's save format. Each repair level fixes its own
// damage and the levels beneath it, and nothing above:
//
//	armor      the lost armor, everywhere (a blown-off or flooded location stays destroyed)
//	structure  also internal structure, critical hits and destroyed equipment: all of them on a unit that is neither crippled nor destroyed (a
//	           destroyed, blown-off or flooded limb and an engine hit included); on a crippled or destroyed one, only those in locations that are not
//	           destroyed, and not the engine's
//	crippled   also destroyed locations and the engine's hits: all damage, so every location, critical slot and inventory item is restored
//	destroyed  also the destroyed flag
//
// crippled says whether the unit is crippled or destroyed, which the state alone can't tell (it takes the engine type). A unit destroyed by its cockpit's loss
// alone (no flag) gets the flag from any lower repair, so that it too needs the destroyed level.
//
// A rearm refills the ammunition and leaves the damage. A service that changes nothing in the unit's state is ErrNoChange.
func ServiceUnitState(force map[string]any, unitID, kind, level string, tons float64, crippled bool, now time.Time) error {
	if ForceVersion(force) == 2 {
		return v2Service(force, unitID, kind, level, tons, crippled)
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
			state, _ := um["state"].(map[string]any)
			if state == nil {
				return errors.New("that unit has no recorded state to change")
			}
			crits, _ := state["crits"].([]any)
			before, _ := json.Marshal(state)
			switch kind {
			case "repair":
				if err := repairState(state, crits, um["unit"], level, tons, crippled); err != nil {
					return err
				}
			case "rearm":
				for _, c := range crits {
					if cm, ok := c.(map[string]any); ok {
						if _, has := cm["consumed"]; has {
							cm["consumed"] = 0
						}
					}
				}
				// Ammunition MekBay keeps on an inventory item (equipment with no critical slot of its own) is refilled too.
				inv, _ := state["inventory"].([]any)
				for _, it := range inv {
					if im, ok := it.(map[string]any); ok {
						delete(im, "consumed")
					}
				}
			default:
				return fmt.Errorf("unknown service %q", kind)
			}
			if after, _ := json.Marshal(state); bytes.Equal(before, after) {
				return ErrNoChange
			}
			um["updatedTs"] = now.UnixMilli()
			return nil
		}
	}
	return errors.New("that unit is no longer in your hangar force")
}

// clearAllDamage restores every location (a blown-off or flooded one too), every critical slot and every destroyed inventory item, and forgets the rule checks
// made on the damage, leaving the destroyed flag and the spent ammunition.
func clearAllDamage(state map[string]any, crits []any) {
	state["locations"] = map[string]any{}
	delete(state, "ruleChecks")
	for _, c := range crits {
		if cm, ok := c.(map[string]any); ok {
			clearCrit(cm)
		}
	}
	inv, _ := state["inventory"].([]any)
	for _, it := range inv {
		if im, ok := it.(map[string]any); ok {
			clearItem(im)
		}
	}
}

// clearCrit takes a critical slot's damage away, pending hits and their timestamps included, and keeps its spent ammunition.
func clearCrit(cm map[string]any) {
	for _, k := range []string{"destroyed", "destroying", "destroyedTurn", "pendingHits", "hitTimestamps", "pendingHitTimestamps"} {
		delete(cm, k)
	}
	if _, has := cm["hits"]; has {
		cm["hits"] = 0
	}
}

// clearItem repairs a destroyed inventory item (pending destruction too), keeping its spent ammunition and settings.
func clearItem(im map[string]any) {
	delete(im, "destroyed")
	delete(im, "destroying")
}

// locationGone says whether a saved location (the decoded JSON of one entry of a state's locations) is destroyed, as InternalGone does: its internal structure is
// gone (pending loss included), or it is blown off or flooded.
func locationGone(loc string, m map[string]any, tons float64) bool {
	if m == nil {
		return false
	}
	if max := maxStructure(loc, tons); max > 0 && numberOf(m["internal"])+numberOf(m["pendingInternal"]) >= max {
		return true
	}
	cs, _ := m["conditions"].([]any)
	for _, c := range cs {
		raw, _ := json.Marshal(c)
		if destroysLocation(conditionKey(raw)) {
			return true
		}
	}
	return false
}

func repairState(state map[string]any, crits []any, unit any, level string, tons float64, crippled bool) error {
	switch level {
	case RepairArmor, RepairStructure, RepairCrippled, RepairDestroyed:
	default:
		return fmt.Errorf("unknown repair level %q", level)
	}
	// A unit counts as destroyed by its cockpit's loss even without MekBay's flag (IsDestroyed). A repair below the destroyed level leaves it destroyed, so the
	// flag is set before the damage that shows it (the head, the cockpit slot) is cleared: only the destroyed level makes it whole, whichever way it was destroyed.
	if level != RepairDestroyed && unitView(state, unit).IsDestroyed() {
		state["destroyed"] = true
	}
	locs, _ := state["locations"].(map[string]any)
	if level == RepairStructure && !crippled {
		// Nothing on this unit belongs to a higher level, so the structure repair restores every location and every critical slot.
		clearAllDamage(state, crits)
		return nil
	}
	switch level {
	case RepairArmor, RepairStructure:
		gone := map[string]bool{}
		for k, v := range locs {
			m, _ := v.(map[string]any)
			if m == nil {
				continue
			}
			gone[k] = locationGone(k, m, tons)
			m["armor"] = 0
			delete(m, "pendingArmor")
			if level == RepairStructure && !gone[k] {
				m["internal"] = 0
				delete(m, "pendingInternal")
			}
			// A blown-off or flooded location stays as it is (its conditions with it) until the crippled level restores it.
			if cs, _ := m["conditions"].([]any); numberOf(m["internal"])+numberOf(m["pendingInternal"]) == 0 && len(cs) == 0 {
				delete(locs, k)
			}
		}
		if level == RepairStructure {
			for _, c := range crits {
				cm, ok := c.(map[string]any)
				if !ok {
					continue
				}
				loc, _ := cm["loc"].(string)
				name, _ := cm["name"].(string)
				if gone[loc] {
					continue // a destroyed location's slots go with the location
				}
				if isEngineSlot(name) {
					continue // engine hits are what cripples a unit
				}
				clearCrit(cm)
			}
			inv, _ := state["inventory"].([]any)
			for _, it := range inv {
				im, ok := it.(map[string]any)
				if !ok {
					continue
				}
				id, _ := im["id"].(string)
				if _, loc := inventoryRef(id); !gone[loc] {
					clearItem(im)
				}
			}
		}
	case RepairCrippled, RepairDestroyed:
		clearAllDamage(state, crits)
		if level == RepairDestroyed {
			state["destroyed"] = false
		}
	}
	return nil
}
