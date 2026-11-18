package site

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Cloning a hangar. The player does not hand the site their MekBay force: they hand it a link, and the site makes its own copy of that force and owns the copy,
// under the MekBay identity derived from their seed (accounts.go). Everything the site does to the force's hangar (repairs, rearms, purchases, the crews it
// assigns) is done to its own copy, so it never needs, or touches, the player's MekBay account, and the player never sees the copy. They can keep arranging
// their own force in their own MekBay tab and re-import it later.
//
// The copy starts clean: every unit fully repaired and rearmed, heat and pilot wounds cleared, and every crew set to an unnamed 4/5 pilot, so the campaign begins
// from the units alone, whatever state the source force was left in.

// CloneRequest asks MekBay to copy a force into a new force owned by the site's account for the seed.
type CloneRequest struct {
	OwnerUID string // the MekBay account that will own the copy (the one account derived from the seed)
	Source   string // the instance ID of the force to copy: anyone can read a force by its ID
	Target   string // the instance ID the copy is saved under
}

// pristineUnit resets one unit of a saved force (the decoded JSON of a unit) to a clean state: every location's armor and structure restored, every critical
// hit and the destroyed flag cleared, spent ammunition refilled, heat and the torso-crippling check cleared, and the crew replaced by one unnamed 4/5 pilot with
// no wounds.
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
	state["turnState"] = map[string]any{"applyMovePSR": false}
	crits, _ := state["crits"].([]any)
	for _, c := range crits {
		if cm, ok := c.(map[string]any); ok {
			clearCrit(cm) // no critical damage, but the slot stays
			if _, has := cm["consumed"]; has {
				cm["consumed"] = 0
			}
		}
	}
	state["crew"] = []any{map[string]any{"id": 0, "name": "", "gunnerySkill": defaultGunnery, "pilotingSkill": defaultPiloting, "hits": 0, "state": 0}}
	um["updatedTs"] = now.UnixMilli()
}

// cloneForce copies a saved force (the decoded JSON of its `data`) as a new force with this instance ID, with every unit made pristine (pristineUnit). The groups,
// the units and their IDs are kept as they are, so the arrangement the player made in MekBay survives; only what the server fills in for readers ("owned") is
// dropped.
func cloneForce(data map[string]any, instance string, now time.Time) (map[string]any, error) {
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
	force["instanceId"] = instance
	force["timestamp"] = mekbayTime(now)
	delete(force, "owned")
	return force, nil
}

// CloneHangar reads a force by its ID and saves a clean copy of it under the owner's account, returning the copy as the site will read it.
func (c *MekBayClient) CloneHangar(ctx context.Context, req CloneRequest) (HangarForce, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := dialMekBay(ctx, c.url(), req.OwnerUID)
	if err != nil {
		return HangarForce{}, err
	}
	defer s.close()
	data, err := s.getForce(ctx, req.Source, false)
	if err != nil {
		return HangarForce{}, err
	}
	if data == nil {
		return HangarForce{}, errors.New("MekBay has no force for that hangar link; check that the link is saved in MekBay")
	}
	force, err := cloneForce(data, req.Target, time.Now())
	if err != nil {
		return HangarForce{}, err
	}
	if err := s.saveData(ctx, force); err != nil {
		return HangarForce{}, err
	}
	b, err := json.Marshal(map[string]any{"data": force})
	if err != nil {
		return HangarForce{}, err
	}
	return parseHangarForce(b)
}
