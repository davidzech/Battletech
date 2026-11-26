package mekbay

import (
	"context"
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
}

// serviceUnitState applies a repair or rearm to the unit with this ID inside a force in MekBay's save format. Each repair level fixes its own
// damage and the levels beneath it, and nothing above:
//
//	armor      the lost armor, everywhere
//	structure  also internal structure in locations that are not destroyed, and critical hits there (but not the engine's, which cripple)
//	crippled   also destroyed locations and the engine's hits: all damage, so every location and every critical slot is cleared
//	destroyed  also the destroyed flag
//
// A rearm refills the ammunition and leaves the damage.
func serviceUnitState(force map[string]any, unitID, kind, level string, tons float64) error {
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
			switch kind {
			case "repair":
				if err := repairState(state, crits, level, tons); err != nil {
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
			um["updatedTs"] = time.Now().UnixMilli()
			return nil
		}
	}
	return errors.New("that unit is no longer in your hangar force")
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

func repairState(state map[string]any, crits []any, level string, tons float64) error {
	locs, _ := state["locations"].(map[string]any)
	loss := func(m map[string]any, key string) int { f, _ := m[key].(float64); return int(f) }
	gone := func(loc string, m map[string]any) bool {
		max := maxStructure(loc, tons)
		return max > 0 && loss(m, "internal") >= max
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
		state["locations"] = map[string]any{}
		delete(state, "ruleChecks")
		for _, c := range crits {
			if cm, ok := c.(map[string]any); ok {
				clearCrit(cm)
			}
		}
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
	return c.editForce(ctx, req.OwnerUID, req.Instance, func(force map[string]any, _ time.Time) error {
		return serviceUnitState(force, req.UnitID, req.Kind, req.Level, req.Tons)
	})
}
