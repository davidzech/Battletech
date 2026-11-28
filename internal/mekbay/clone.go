package mekbay

import (
	"encoding/json"
	"time"
)

// Importing a hangar from a link. The player does not hand the site their MekBay force: they hand it a link, and the site reads that force once and keeps a copy
// of its own in its database as the campaign's hangar. Everything the site does to the hangar (repairs, rearms, purchases, battles) is done to its copy, so it
// never touches the player's MekBay force, which they can keep arranging in their own MekBay tab.
//
// The copy starts clean: every unit fully repaired and rearmed, heat and pilot wounds cleared, and every crew set to an unnamed 4/5 pilot, so the campaign begins
// from the units alone, whatever state the source force was left in.

// unnamedCrew is the crew of a unit nobody in particular drives, in MekBay's save format: one unnamed pilot with the default skills and no wounds.
func unnamedCrew() []any {
	return []any{map[string]any{"id": 0, "name": "", "gunnerySkill": DefaultGunnery, "pilotingSkill": DefaultPiloting, "hits": 0, "state": 0}}
}

// pristineUnit resets one unit of a saved force (the decoded JSON of a unit) to a clean state: every location's armor and structure restored (its conditions
// with it), every critical hit (pending hits and their timestamps too) and the destroyed flag cleared, spent ammunition refilled, the inventory's state, heat, the
// turn's state, the unit's conditions and the torso-crippling check dropped, and the crew replaced by one unnamed 4/5 pilot with no wounds.
func pristineUnit(um map[string]any, now time.Time) {
	state, _ := um["state"].(map[string]any)
	if state == nil {
		state = map[string]any{}
		um["state"] = state
	}
	state["locations"] = map[string]any{}
	state["destroyed"] = false
	delete(state, "ruleChecks")
	state["heat"] = map[string]any{"current": 0, "previous": 0}
	delete(state, "turnState")
	delete(state, "conditions")
	delete(state, "inventory") // destroyed equipment, spent ammunition and settings of the unit's inventory: MekBay rebuilds a missing inventory as new
	crits, _ := state["crits"].([]any)
	for _, c := range crits {
		if cm, ok := c.(map[string]any); ok {
			clearCrit(cm) // no critical damage, but the slot stays
			if _, has := cm["consumed"]; has {
				cm["consumed"] = 0
			}
		}
	}
	state["crew"] = unnamedCrew()
	um["updatedTs"] = now.UnixMilli()
}

// CleanForce copies a saved force (the decoded JSON of its `data`) as a new force with this instance ID, with every unit made pristine (pristineUnit). The groups,
// the units and their IDs are kept as they are, so the arrangement the player made in MekBay survives; only what the server fills in for readers ("owned") is
// dropped.
func CleanForce(data map[string]any, instance string, now time.Time) (map[string]any, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var force map[string]any
	if err := json.Unmarshal(raw, &force); err != nil {
		return nil, err
	}
	groups, _ := force["groups"].([]any)
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		units, _ := gm["units"].([]any)
		for _, u := range units {
			if um, ok := u.(map[string]any); ok {
				pristineUnit(um, now)
			}
		}
	}
	if ForceVersion(force) == 2 {
		v2Clean(force, instance, now)
		return force, nil
	}
	force["instanceId"] = instance
	force["timestamp"] = FormatTime(now)
	delete(force, "owned")
	return force, nil
}
