package mekbay

import (
	"fmt"
	"strings"
)

// CrewAssignment puts a named pilot in a unit's crew. An empty Name clears the unit back to an unnamed 4/5 crew.
type CrewAssignment struct {
	UnitID   string
	Name     string
	Gunnery  int
	Piloting int
}

const (
	DefaultGunnery  = 4
	DefaultPiloting = 5
	MaxAssignments  = 24 // a mistake guard on how many units one deploy may re-crew
)

// applyCrew writes assignments into a saved force (the decoded JSON of its `data`). Every assigned pilot is first taken off any other unit
// they were crewing, so a pilot is never in two mechs; a unit named in the assignments but missing from the force is an error.
func applyCrew(force map[string]any, crew []CrewAssignment) error {
	if ForceVersion(force) == 2 {
		return v2ApplyCrew(force, crew)
	}
	if len(crew) == 0 {
		return nil
	}
	target := map[string]CrewAssignment{}
	named := map[string]bool{}
	for _, a := range crew {
		target[a.UnitID] = a
		if a.Name != "" {
			named[strings.ToLower(a.Name)] = true
		}
	}
	setCrew := func(um map[string]any, name string, gunnery, piloting int) {
		state, _ := um["state"].(map[string]any)
		if state == nil {
			state = map[string]any{}
			um["state"] = state
		}
		members, _ := state["crew"].([]any)
		var first map[string]any
		if len(members) > 0 {
			first, _ = members[0].(map[string]any)
		}
		if first == nil {
			first = map[string]any{"id": 0, "hits": 0, "state": 0}
			members = append([]any{first}, members...)
			state["crew"] = members
		}
		first["name"], first["gunnerySkill"], first["pilotingSkill"] = name, gunnery, piloting
	}
	found := map[string]bool{}
	groups, _ := force["groups"].([]any)
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		units, _ := gm["units"].([]any)
		for _, u := range units {
			um, _ := u.(map[string]any)
			if um == nil {
				continue
			}
			id, _ := um["id"].(string)
			if a, ok := target[id]; ok {
				found[id] = true
				if a.Name == "" {
					setCrew(um, "", DefaultGunnery, DefaultPiloting)
				} else {
					setCrew(um, a.Name, a.Gunnery, a.Piloting)
				}
				continue
			}
			// Not being assigned: if a pilot who is being assigned elsewhere is on this unit, they leave it.
			if state, _ := um["state"].(map[string]any); state != nil {
				if members, _ := state["crew"].([]any); len(members) > 0 {
					if first, _ := members[0].(map[string]any); first != nil {
						if n, _ := first["name"].(string); n != "" && named[strings.ToLower(n)] {
							setCrew(um, "", DefaultGunnery, DefaultPiloting)
						}
					}
				}
			}
		}
	}
	for id := range target {
		if !found[id] {
			return fmt.Errorf("unit %s is not in the hangar force", id)
		}
	}
	return nil
}

// setCommander marks one unit of a saved force (the decoded JSON of its `data`) as MekBay's commander (`commander: true` on the unit, which the record sheet
// shows) and takes the mark off every other unit, so a force copied from the hangar never keeps an old one. An empty unitID only clears it.
func setCommander(force map[string]any, unitID string) {
	if ForceVersion(force) == 2 {
		v2SetCommander(force, unitID)
		return
	}
	groups, _ := force["groups"].([]any)
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		units, _ := gm["units"].([]any)
		for _, u := range units {
			um, _ := u.(map[string]any)
			if um == nil {
				continue
			}
			if id, _ := um["id"].(string); unitID != "" && id == unitID {
				um["commander"] = true
			} else {
				delete(um, "commander")
			}
		}
	}
}
