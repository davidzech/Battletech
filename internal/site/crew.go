package site

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/davidzech/Battletech/internal/mekbay"

	"github.com/davidzech/Battletech/internal/pilot"
)

// Assigning named pilots to mechs. MekBay already stores a crew on every unit (a name and Gunnery and Piloting skills), so an assignment is just
// that: the named pilot's name and their current skill levels written into the unit's crew in the hangar force. The player arranges the
// assignments in the Deploy popup and they are saved to MekBay with the deploy. One pilot drives one mech at a time, so assigning a pilot to a
// unit takes them off any other unit; a unit with no named pilot goes back to an unnamed 4/5 crew.

// parseCrew reads the pairs of assign_unit and assign_pilot fields a deploy carries into assignments, checking them against the hangar and the
// campaign's living named pilots. The skills come from the pilot's current levels, never from the request.
func parseCrew(r *http.Request, h mekbay.HangarForce, pilots []pilot.NamedPilot) ([]mekbay.CrewAssignment, error) {
	return parseCrewPairs(r.PostForm["assign_unit"], r.PostForm["assign_pilot"], h, pilots)
}

// parseCrewPairs is parseCrew over already-split fields: units[i] gets the pilot names[i] (empty clears it).
func parseCrewPairs(units, names []string, h mekbay.HangarForce, pilots []pilot.NamedPilot) ([]mekbay.CrewAssignment, error) {
	if len(units) != len(names) {
		return nil, errors.New("The pilot assignments are incomplete.")
	}
	if len(units) > mekbay.MaxAssignments {
		return nil, errors.New("Too many pilot assignments.")
	}
	inHangar := map[string]bool{}
	for _, u := range h.Units {
		inHangar[u.ID] = true
	}
	byName := map[string]pilot.NamedPilot{}
	for _, p := range pilots {
		if !p.Fallen {
			byName[strings.ToLower(p.Name)] = p
		}
	}
	var out []mekbay.CrewAssignment
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
			out = append(out, mekbay.CrewAssignment{UnitID: id})
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
		out = append(out, mekbay.CrewAssignment{UnitID: id, Name: p.Name, Gunnery: p.Gunnery(), Piloting: p.Piloting()})
	}
	return out, nil
}
