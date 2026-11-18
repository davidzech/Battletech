package site

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
			delete(ns, "crew") // pilots are assigned during an operation only: a unit back in the hangar has the default, unnamed crew
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

// CommitBattle reads the battle's operation, takes the state of every unit in the players' forces and saves it into the matching units of the hangar force.
func (c *MekBayClient) CommitBattle(ctx context.Context, req CommitBattleRequest) (CommitBattleReport, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	s, err := dialMekBay(ctx, c.url(), req.OwnerUID)
	if err != nil {
		return CommitBattleReport{}, err
	}
	defer s.close()
	raw, err := s.call(ctx, map[string]any{"action": "getOperation", "operationId": req.OperationID})
	if err != nil {
		return CommitBattleReport{}, err
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
		return CommitBattleReport{}, errors.New("MekBay has no such operation any more")
	}
	read := func(instance string) (map[string]any, error) {
		got, err := s.call(ctx, map[string]any{"action": "getForce", "uuid": req.OwnerUID, "instanceId": instance, "ownedOnly": false})
		if err != nil {
			return nil, err
		}
		var reply struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(got, &reply); err != nil || reply.Data == nil {
			return nil, fmt.Errorf("MekBay has no force %s", instance)
		}
		return reply.Data, nil
	}
	battle := map[string]map[string]any{}
	for _, f := range op.Data.Forces {
		if f.Alignment != "friendly" {
			continue
		}
		force, err := read(f.InstanceID)
		if err != nil {
			return CommitBattleReport{}, err
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
		return CommitBattleReport{}, errors.New("none of the players' forces could be read from MekBay")
	}
	hangar, err := read(req.HangarInstance)
	if err != nil {
		return CommitBattleReport{}, errors.New("MekBay has no force for your hangar; try importing it again")
	}
	rep := commitState(hangar, battle)
	if rep.Matched == 0 {
		return rep, nil
	}
	delete(hangar, "owned")
	hangar["timestamp"] = mekbayTime(time.Now())
	if _, err := s.call(ctx, map[string]any{"action": "saveForce", "uuid": req.OwnerUID, "data": hangar, "savedForceCount": 1}); err != nil {
		return rep, err
	}
	return rep, nil
}
