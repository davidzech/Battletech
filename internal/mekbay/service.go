package mekbay

import (
	"bytes"
	"context"
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

// ServiceRequest asks MekBay to clear a unit's damage (repair) or refill its ammunition (rearm) in the player's hangar force.
type ServiceRequest struct {
	OwnerUID string
	Instance string
	UnitID   string
	Kind     string  // "repair" or "rearm"
	Level    string  // for a repair, the level (armor, structure, crippled, destroyed)
	Tons     float64 // the unit's tonnage, to tell which locations are destroyed
	Crippled bool    // the unit is crippled or destroyed (see ConditionOf), which leaves its destroyed locations and engine hits to the crippled level
}

// ErrNoChange is what an edit to a unit answers when the force as it was just read leaves it nothing to do: the repair, rearm or reconfiguration asked for has
// already been made (by a second click, or by another player at the table). Nothing is saved, and the site gives the SP back.
var ErrNoChange = errors.New("That has already been done to the unit (it may have changed). Reload and check your hangar.")

// serviceUnitState applies a repair or rearm to the unit with this ID inside a force in MekBay's save format. Each repair level fixes its own
// damage and the levels beneath it, and nothing above:
//
//	armor      the lost armor, everywhere
//	structure  also internal structure and critical hits: all of them on a unit that is neither crippled nor destroyed (a destroyed limb and an
//	           engine hit included); on a crippled or destroyed one, only those in locations that are not destroyed, and not the engine's
//	crippled   also destroyed locations and the engine's hits: all damage, so every location and every critical slot is cleared
//	destroyed  also the destroyed flag
//
// crippled says whether the unit is crippled or destroyed, which the state alone can't tell (it takes the engine type).
//
// A rearm refills the ammunition and leaves the damage. A service that changes nothing in the unit's state is ErrNoChange.
func serviceUnitState(force map[string]any, unitID, kind, level string, tons float64, crippled bool, now time.Time) error {
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
				if err := repairState(state, crits, level, tons, crippled); err != nil {
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

// clearAllDamage restores every location and every critical slot (and forgets the rule checks made on the damage), leaving the destroyed flag.
func clearAllDamage(state map[string]any, crits []any) {
	state["locations"] = map[string]any{}
	delete(state, "ruleChecks")
	for _, c := range crits {
		if cm, ok := c.(map[string]any); ok {
			clearCrit(cm)
		}
	}
}

// clearCrit takes a critical slot's damage away and keeps its spent ammunition.
func clearCrit(cm map[string]any) {
	delete(cm, "destroyed")
	delete(cm, "destroying")
	delete(cm, "destroyedTurn")
	if _, has := cm["hits"]; has {
		cm["hits"] = 0
	}
}

func repairState(state map[string]any, crits []any, level string, tons float64, crippled bool) error {
	locs, _ := state["locations"].(map[string]any)
	loss := func(m map[string]any, key string) int { f, _ := m[key].(float64); return int(f) }
	gone := func(loc string, m map[string]any) bool {
		max := maxStructure(loc, tons)
		return max > 0 && loss(m, "internal") >= max
	}
	if level == RepairStructure && !crippled {
		// Nothing on this unit belongs to a higher level, so the structure repair restores every location and every critical slot.
		clearAllDamage(state, crits)
		return nil
	}
	switch level {
	case RepairArmor, RepairStructure:
		for k, v := range locs {
			m, _ := v.(map[string]any)
			if m == nil {
				continue
			}
			m["armor"] = 0
			if level == RepairStructure && !gone(k, m) {
				m["internal"] = 0
			}
			if loss(m, "internal") == 0 {
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
				if lm, _ := locs[loc].(map[string]any); lm != nil && gone(loc, lm) {
					continue // a destroyed location's slots go with the location
				}
				if isEngineSlot(name) {
					continue // engine hits are what cripples a unit
				}
				clearCrit(cm)
			}
		}
	case RepairCrippled, RepairDestroyed:
		clearAllDamage(state, crits)
		if level == RepairDestroyed {
			state["destroyed"] = false
		}
	default:
		return fmt.Errorf("unknown repair level %q", level)
	}
	return nil
}

// ServiceUnit repairs or rearms a unit in the player's hangar force. It needs the owner's UID, because MekBay only accepts the owner's saves.
func (c *Client) ServiceUnit(ctx context.Context, req ServiceRequest) (HangarForce, error) {
	return c.editForce(ctx, req.OwnerUID, req.Instance, func(force map[string]any, now time.Time) error {
		return serviceUnitState(force, req.UnitID, req.Kind, req.Level, req.Tons, req.Crippled, now)
	})
}
