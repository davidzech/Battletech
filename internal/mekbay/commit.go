package mekbay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// Writing a finished battle back into the hangar.
//
// A battle's forces are copies of hangar units made by subsetForce, which keeps each unit's own ID inside the force. That ID is the match: the hangar unit with the
// same ID (and the same design, as a check) is the one the battle's unit came from, so two Orions in the hangar are never confused (they have different IDs, and the
// force each player has holds only the ones that player took). A unit the hangar no longer has (sold, or swapped for another variant, while the battle was on) is
// skipped and reported.

// CommitBattleRequest asks for the battle's units to be written back into the hangar force.
type CommitBattleRequest struct {
	OwnerUID       string // the account that owns the hangar copy
	HangarInstance string
	OperationID    string
	// Skip are friendly forces of the operation whose units never go back into the hangar: the scenario's allies, which aren't the players' mechs.
	Skip []string
}

// CommitBattleReport says what was written back.
type CommitBattleReport struct {
	Matched int      // units whose record sheet state went back into the hangar
	Skipped []string // battle units with no match in the hangar: "<design> (<unit id>)"
}

// commitState copies what a battle recorded on its units (the state: damage, critical hits, ammunition, rule checks) onto the hangar's units with the same ID and
// design. The crew is not carried back and the unit's crew is cleared: pilots are assigned during an operation only, so the hangar never holds a named pilot.
func commitState(hangar map[string]any, battle map[string]map[string]any) CommitBattleReport {
	var rep CommitBattleReport
	used := map[string]bool{}
	groups, _ := hangar["groups"].([]any)
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		us, _ := gm["units"].([]any)
		for _, u := range us {
			um, _ := u.(map[string]any)
			id, _ := um["id"].(string)
			bu, ok := battle[id]
			if !ok || bu["unit"] != um["unit"] {
				continue
			}
			state, _ := bu["state"].(map[string]any)
			next, err := deepCopy(map[string]any{"s": state})
			if err != nil {
				continue
			}
			ns, _ := next["s"].(map[string]any)
			if ns == nil {
				ns = map[string]any{}
			}
			// Pilots are assigned during an operation only: a unit back in the hangar has the default, unnamed crew. MekBay needs the crew entry to be there, so it is
			// reset rather than removed (the hangar's own entries if it has them, else the battle's).
			if old, _ := um["state"].(map[string]any); old != nil && old["crew"] != nil {
				ns["crew"] = old["crew"]
			}
			resetCrew(ns)
			um["state"] = ns
			used[id] = true
			rep.Matched++
		}
	}
	for id, bu := range battle {
		if !used[id] {
			rep.Skipped = append(rep.Skipped, fmt.Sprintf("%v (%s)", bu["unit"], id))
		}
	}
	return rep
}

// errNothingToCommit stops editForce from saving a hangar none of the battle's units matched.
var errNothingToCommit = errors.New("nothing to write back")

// CommitBattle reads the battle's operation, takes the state of every unit in the players' forces and saves it into the matching units of the hangar force,
// through editForce like every other hangar edit. It returns the hangar as saved, for the site's cached copy; when no unit matched nothing is saved and the
// hangar returned is the zero HangarForce.
//
// A hangar MekBay no longer has is reported as such (import it again); any other failure to read it (a timeout, say) is passed on as it is, so the player tries
// again rather than re-importing.
func (c *Client) CommitBattle(ctx context.Context, req CommitBattleRequest) (CommitBattleReport, HangarForce, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	battle, err := c.readBattleUnits(ctx, req)
	if err != nil {
		return CommitBattleReport{}, HangarForce{}, err
	}
	var rep CommitBattleReport
	h, err := c.editForce(ctx, req.OwnerUID, req.HangarInstance, func(hangar map[string]any, _ time.Time) error {
		if rep = commitState(hangar, battle); rep.Matched == 0 {
			return errNothingToCommit
		}
		return nil
	})
	switch {
	case errors.Is(err, errNothingToCommit):
		return rep, HangarForce{}, nil
	case errors.Is(err, errNoHangarForce):
		return CommitBattleReport{}, HangarForce{}, errors.New("MekBay has no force for your hangar; try importing it again")
	case err != nil:
		return CommitBattleReport{}, HangarForce{}, err
	}
	return rep, h, nil
}

// readBattleUnits reads the battle's operation and the players' forces in it (every friendly force but those req.Skip names), and answers their units by ID.
func (c *Client) readBattleUnits(ctx context.Context, req CommitBattleRequest) (map[string]map[string]any, error) {
	s, err := Dial(ctx, c.url(), req.OwnerUID)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	raw, err := s.Call(ctx, map[string]any{"action": "getOperation", "operationId": req.OperationID})
	if err != nil {
		return nil, err
	}
	var op struct {
		Data *struct {
			Forces []struct {
				InstanceID string `json:"instanceId"`
				Alignment  string `json:"alignment"`
			} `json:"forces"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &op); err != nil || op.Data == nil {
		return nil, errors.New("MekBay has no such operation any more")
	}
	battle := map[string]map[string]any{}
	for _, f := range op.Data.Forces {
		if f.Alignment != "friendly" || slices.Contains(req.Skip, f.InstanceID) {
			continue
		}
		force, err := s.getForce(ctx, f.InstanceID, false)
		if err != nil {
			return nil, err
		}
		if force == nil {
			return nil, fmt.Errorf("MekBay has no force %s", f.InstanceID)
		}
		groups, _ := force["groups"].([]any)
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			us, _ := gm["units"].([]any)
			for _, u := range us {
				if um, _ := u.(map[string]any); um != nil {
					if id, _ := um["id"].(string); id != "" {
						battle[id] = um
					}
				}
			}
		}
	}
	if len(battle) == 0 {
		return nil, errors.New("none of the players' forces could be read from MekBay")
	}
	return battle, nil
}

// resetCrew turns every crew member of a unit's state into the default one: no name, Gunnery 4, Piloting 5, unhurt (a 4/5 pilot is what MekBay gives a new unit).
func resetCrew(state map[string]any) {
	crew, _ := state["crew"].([]any)
	for _, c := range crew {
		if cm, ok := c.(map[string]any); ok {
			cm["name"], cm["gunnerySkill"], cm["pilotingSkill"], cm["hits"], cm["state"] = "", DefaultGunnery, DefaultPiloting, 0, 0
		}
	}
}
