package site

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Assigning named pilots to mechs. MekBay already stores a crew on every unit (a name and Gunnery and Piloting skills), so an assignment is just
// that: the named pilot's name and their current skill levels written into the unit's crew in the hangar force. The player arranges the
// assignments in the Deploy popup and they are saved to MekBay with the deploy. One pilot drives one mech at a time, so assigning a pilot to a
// unit takes them off any other unit; a unit with no named pilot goes back to an unnamed 4/5 crew.

// CrewAssignment puts a named pilot in a unit's crew. An empty Name clears the unit back to an unnamed 4/5 crew.
type CrewAssignment struct {
	UnitID   string
	Name     string
	Gunnery  int
	Piloting int
}

const (
	defaultGunnery  = 4
	defaultPiloting = 5
	maxAssignments  = 24 // a mistake guard on how many units one deploy may re-crew
)

// applyCrew writes assignments into a saved force (the decoded JSON of its `data`). Every assigned pilot is first taken off any other unit
// they were crewing, so a pilot is never in two mechs; a unit named in the assignments but missing from the force is an error.
func applyCrew(force map[string]any, crew []CrewAssignment) error {
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
					setCrew(um, "", defaultGunnery, defaultPiloting)
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
							setCrew(um, "", defaultGunnery, defaultPiloting)
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

// parseCrew reads the pairs of assign_unit and assign_pilot fields a deploy carries into assignments, checking them against the hangar and the
// campaign's living named pilots. The skills come from the pilot's current levels, never from the request.
func parseCrew(r *http.Request, h HangarForce, pilots []NamedPilot) ([]CrewAssignment, error) {
	return parseCrewPairs(r.PostForm["assign_unit"], r.PostForm["assign_pilot"], h, pilots)
}

// parseCrewPairs is parseCrew over already-split fields: units[i] gets the pilot names[i] (empty clears it).
func parseCrewPairs(units, names []string, h HangarForce, pilots []NamedPilot) ([]CrewAssignment, error) {
	if len(units) != len(names) {
		return nil, errors.New("The pilot assignments are incomplete.")
	}
	if len(units) > maxAssignments {
		return nil, errors.New("Too many pilot assignments.")
	}
	inHangar := map[string]bool{}
	for _, u := range h.Units {
		inHangar[u.ID] = true
	}
	byName := map[string]NamedPilot{}
	for _, p := range pilots {
		if !p.Fallen {
			byName[strings.ToLower(p.Name)] = p
		}
	}
	var out []CrewAssignment
	seenUnit, seenPilot := map[string]bool{}, map[string]bool{}
	for i, id := range units {
		if !inHangar[id] {
			return nil, errors.New("A unit you assigned a pilot to is no longer in your hangar. Reopen the popup to reload it.")
		}
		if seenUnit[id] {
			return nil, errors.New("A unit was given two pilots.")
		}
		seenUnit[id] = true
		name := strings.TrimSpace(names[i])
		if name == "" {
			out = append(out, CrewAssignment{UnitID: id})
			continue
		}
		p, ok := byName[strings.ToLower(name)]
		if !ok {
			return nil, fmt.Errorf("%q is not one of your living named pilots.", name)
		}
		if seenPilot[strings.ToLower(p.Name)] {
			return nil, fmt.Errorf("%s was assigned to two mechs.", p.Name)
		}
		seenPilot[strings.ToLower(p.Name)] = true
		out = append(out, CrewAssignment{UnitID: id, Name: p.Name, Gunnery: p.Gunnery(), Piloting: p.Piloting()})
	}
	return out, nil
}
