package mekbay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Starting a battle: what a Deploy does once the plan is checked. In MekBay it builds a force for each player and one for the OpFor, and saves an operation that
// sets the players' forces against the OpFor's, so the whole table opens one link (`?operation=<id>`) and sees the same scenario. Every force and the operation belong
// to the seed's one account (see accounts.go), so anyone with the link can edit any of them, and MekBay pushes each save to every device that has the force loaded
// (checked live, 2026-11: a save by one connection reaches the others as an `updatedForce` message, and a repair made in one browser showed up in another). An
// operation is a saved document, so unlike a lobby it needs no connection kept open: it can be reopened later, and starting again just updates it.
//
// Each player has a force of their own, not a group of one shared force, because MekBay saves a force as a whole document: two people editing different forces
// can't overwrite each other.
//
// (A multiplayer lobby was built and verified first and then dropped: lobbies exist only while someone is connected, an account can be in only one, and a start
// that stopped halfway left accounts stuck in it. Its notes: alignment is per viewer, not set by the host; membership belongs to a live connection; createLobby
// gives a 4-character code; syncLobbyForces publishes a participant's forces.)

// BattleRequest is everything a battle needs in MekBay.
type BattleRequest struct {
	Account string // the seed's MekBay account: it owns every force and the operation
	// Hangar is the campaign's hangar (its saved data, as the site stores it) the players' mechs are taken from (not used when Provided).
	Hangar []byte
	// Provided says the mechs are not the hangar's (a scenario that provides its own): each player's force is built from its Draft instead of copied from the hangar.
	Provided bool
	Players  []BattlePlayer
	OpFor    BattleOpFor
	// Allies are the scenario's own units on the players' side that no player controls (its automata); they are built new, as a force of their own after the
	// players' forces, on the friendly side. No groups means none.
	Allies        ForceDraft
	OperationName string
	// OperationID is the operation of an earlier start of this battle to update, so the link stays the same; "" makes a new one.
	OperationID string
}

// BattlePlayer is one player's force.
type BattlePlayer struct {
	Number int    // 1 to 4
	Name   string // the force's name
	// Units are the hangar units the player takes, with the crews they were given. Draft is the same force built from scratch, for a Provided battle.
	Units []string
	Crew  []CrewAssignment
	Draft ForceDraft
	// Commander is the hangar unit (one of Units) whose pilot commands the whole force, marked as MekBay's commander; "" when it isn't in this player's force.
	// A Provided battle marks it in the Draft instead.
	Commander string
}

// BattleOpFor is the OpFor's force.
type BattleOpFor struct {
	Draft ForceDraft
	// Reuse is the OpFor force of an earlier start of this battle to keep (with whatever damage it has recorded); "" builds a fresh one.
	Reuse string
}

// BattleResult says what was created.
type BattleResult struct {
	OperationID     string
	OpForInstance   string
	PlayerInstances []string // each player's force, in the order of the request
	AlliesInstance  string   // the allies' force ("" when the scenario has none)
}

// subsetForce copies the units a player takes out of the hangar force (the decoded JSON of its saved data) into a force of their own, keeping what MekBay
// records on each unit (damage, ammunition, crew), under a new instance ID and name, all in one group.
func subsetForce(hangar map[string]any, units []string, instance, name string, now time.Time) (map[string]any, error) {
	want := map[string]bool{}
	for _, id := range units {
		want[id] = true
	}
	var taken []any
	groups, _ := hangar["groups"].([]any)
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		us, _ := gm["units"].([]any)
		for _, u := range us {
			um, _ := u.(map[string]any)
			if id, _ := um["id"].(string); want[id] {
				c, err := deepCopy(um)
				if err != nil {
					return nil, err
				}
				taken = append(taken, c)
				delete(want, id)
			}
		}
	}
	if len(want) > 0 {
		missing := make([]string, 0, len(want))
		for id := range want {
			missing = append(missing, id)
		}
		slices.Sort(missing)
		return nil, fmt.Errorf("units not in the hangar force: %s", strings.Join(missing, ", "))
	}
	f := map[string]any{}
	for k, v := range hangar {
		switch k {
		case "groups", "owned", "instanceId", "name", "timestamp":
		default:
			f[k] = v
		}
	}
	f["instanceId"], f["name"], f["timestamp"] = instance, name, FormatTime(now)
	f["groups"] = []any{map[string]any{"id": NewID(), "name": "Deployed", "color": "", "units": taken}}
	return f, nil
}

// forceTimestamp is a saved force's timestamp as an operation records it: version 1 keeps it as such, version 2 as milliseconds.
func forceTimestamp(force map[string]any) string {
	switch ts := force["timestamp"].(type) {
	case string:
		return ts
	case float64:
		return FormatTime(time.UnixMilli(int64(ts)))
	}
	return ""
}

func deepCopy(m map[string]any) (map[string]any, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	return out, json.Unmarshal(b, &out)
}

// saveOperation saves an operation and reads it back to confirm it (MekBay doesn't acknowledge saveOperation).
func (s *Session) saveOperation(ctx context.Context, op map[string]any, opID string) error {
	if err := s.Send(ctx, map[string]any{"action": "saveOperation", "forcePersistenceRevision": PersistenceRevision, "data": op}); err != nil {
		return err
	}
	for attempt := 0; attempt < 6; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}
		got, err := s.Call(ctx, map[string]any{"action": "getOperation", "operationId": opID})
		if err != nil {
			return err
		}
		var back struct {
			Data *struct{} `json:"data"`
		}
		if json.Unmarshal(got, &back) == nil && back.Data != nil {
			return nil
		}
	}
	return errors.New("MekBay did not confirm the operation was saved")
}

// StartBattle builds the players' forces and the OpFor's and saves the operation that pits them against each other (see the notes at the top of this file).
//
// The players' forces are copied from the hangar the request carries (the site's database holds it), never read from MekBay. Every force is built and checked
// before the first one is saved, so a plan that can't be built (a unit that is not in the hangar, a crew for a mech that isn't
// there) saves nothing. If a save then fails part-way, the forces this start has already saved (and its operation, if it is a new one) are deleted again, since
// nothing would remember them: the caller gets no result to record.
func (c *Client) StartBattle(ctx context.Context, req BattleRequest) (BattleResult, error) {
	if len(req.Players) < 1 || len(req.Players) > MaxPlayers {
		return BattleResult{}, fmt.Errorf("a battle has 1 to %d players", MaxPlayers)
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	s, err := Dial(ctx, c.url(), req.Account)
	if err != nil {
		return BattleResult{}, err
	}
	defer s.Close()
	get := func(instance string, ownedOnly bool) (map[string]any, error) {
		return s.getForce(ctx, instance, ownedOnly)
	}
	// built is a force ready to be saved: nothing is saved until all of them are.
	type built struct {
		instance, timestamp string
		data                any
	}

	// The OpFor's force: the one from an earlier start of this battle, or a fresh one.
	res := BattleResult{OpForInstance: req.OpFor.Reuse}
	var opforTS string
	var toSave []built
	if res.OpForInstance != "" {
		d, err := get(res.OpForInstance, true)
		if err != nil {
			return BattleResult{}, err
		}
		if d == nil {
			return BattleResult{}, errors.New("the OpFor force from the last start of this battle is gone from MekBay; reset the OpFor to build a new one")
		}
		opforTS = forceTimestamp(d)
	} else {
		res.OpForInstance = NewID()
		now := time.Now().UTC()
		f, err := buildDraft(res.OpForInstance, now, req.OpFor.Draft)
		if err != nil {
			return BattleResult{}, err
		}
		opforTS = FormatTime(now)
		toSave = append(toSave, built{res.OpForInstance, opforTS, f})
	}

	// Each player's force, from the hangar or built from their draft.
	var hangar map[string]any
	if !req.Provided {
		if err := json.Unmarshal(req.Hangar, &hangar); err != nil || hangar == nil {
			return BattleResult{}, errors.New("your hangar could not be read")
		}
		// Writing version 2: a hangar still in version 1 goes into the battle converted (it is converted for good the next time it is changed).
		if err := UpgradeHangar(hangar); err != nil {
			return BattleResult{}, fmt.Errorf("your hangar could not be converted for MekBay: %w", err)
		}
	}
	forces := []map[string]any{}
	for _, p := range req.Players {
		instance, ts := NewID(), ""
		var force any
		now := time.Now().UTC()
		ts = FormatTime(now)
		if req.Provided {
			f, err := buildDraft(instance, now, p.Draft)
			if err != nil {
				return BattleResult{}, err
			}
			force = f
		} else {
			var f map[string]any
			var err error
			if ForceVersion(hangar) == 2 {
				f, err = v2Subset(hangar, p.Units, instance, p.Name, now)
			} else {
				f, err = subsetForce(hangar, p.Units, instance, p.Name, now)
			}
			if err != nil {
				return BattleResult{}, err
			}
			if err := applyCrew(f, p.Crew); err != nil {
				return BattleResult{}, err
			}
			setCommander(f, p.Commander)
			force = f
		}
		toSave = append(toSave, built{instance, ts, force})
		res.PlayerInstances = append(res.PlayerInstances, instance)
		forces = append(forces, map[string]any{"instanceId": instance, "alignment": "friendly", "timestamp": ts})
	}

	// The scenario's allies: one more force on the players' side, after theirs.
	if len(req.Allies.Groups) > 0 {
		res.AlliesInstance = NewID()
		now := time.Now().UTC()
		f, err := buildDraft(res.AlliesInstance, now, req.Allies)
		if err != nil {
			return BattleResult{}, err
		}
		toSave = append(toSave, built{res.AlliesInstance, FormatTime(now), f})
		forces = append(forces, map[string]any{"instanceId": res.AlliesInstance, "alignment": "friendly", "timestamp": FormatTime(now)})
	}

	// Everything could be built, so it is saved: the forces, then the operation with every player's force (and the allies') on the friendly side and the OpFor
	// on the other. From the first save on, a failure deletes what this start saved.
	res.OperationID = req.OperationID
	newOperation := ""
	if res.OperationID == "" {
		res.OperationID = NewID()
		newOperation = res.OperationID
	}
	var saved []string
	for _, f := range toSave {
		// Counted before it is saved: a save that fails may have gone through all the same.
		saved = append(saved, f.instance)
		if err := s.saveData(ctx, f.data); err != nil {
			c.discardBattle(ctx, req.Account, saved, "")
			return BattleResult{}, err
		}
	}
	forces = append(forces, map[string]any{"instanceId": res.OpForInstance, "alignment": "enemy", "timestamp": opforTS})
	op := map[string]any{"operationId": res.OperationID, "name": req.OperationName, "note": "", "timestamp": time.Now().UnixMilli(), "forces": forces}
	if err := s.saveOperation(ctx, op, res.OperationID); err != nil {
		c.discardBattle(ctx, req.Account, saved, newOperation)
		return BattleResult{}, err
	}
	return res, nil
}

// discardBattle deletes the forces a failed StartBattle saved, and its operation if operationID is given, best effort (MekBay does not acknowledge a deletion).
// It uses a connection and a deadline of its own, since the battle's may be what failed.
func (c *Client) discardBattle(ctx context.Context, account string, instances []string, operationID string) {
	if len(instances) == 0 && operationID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	s, err := Dial(ctx, c.url(), account)
	if err != nil {
		return
	}
	defer s.Close()
	for _, in := range instances {
		if err := s.Send(ctx, delForceMessage(account, in)); err != nil {
			return
		}
	}
	if operationID != "" {
		if err := s.Send(ctx, map[string]any{"action": "delOperation", "operationId": operationID}); err != nil {
			return
		}
	}
	// A read on the same connection comes back after the deletions have been handled, so closing the connection does not cut them off.
	if operationID != "" {
		_, _ = s.Call(ctx, map[string]any{"action": "getOperation", "operationId": operationID})
		return
	}
	_, _ = s.Call(ctx, getForceMessage(account, instances[len(instances)-1], true))
}

// BattleForce is one force of a battle as MekBay has it now.
type BattleForce struct {
	InstanceID string
	Alignment  string // "friendly": only the players' forces (and the allies') are read, the OpFor's is not
	Force      HangarForce
}

// BattleStatus is a battle's operation read back: the players' forces in it, in the operation's order.
type BattleStatus struct {
	Forces []BattleForce
}

// ReadBattle reads an operation and each of its players' forces anonymously (anyone can read what they have the IDs of), which is how the site shows how a
// battle stands: MekBay holds the damage, ammunition and crews, and players change them there. The OpFor's force is not read, since the page doesn't show it.
func (c *Client) ReadBattle(ctx context.Context, operationID string) (BattleStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := Dial(ctx, c.url(), NewID())
	if err != nil {
		return BattleStatus{}, err
	}
	defer s.Close()
	raw, err := s.Call(ctx, map[string]any{"action": "getOperation", "operationId": operationID})
	if err != nil {
		return BattleStatus{}, err
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
		return BattleStatus{}, errors.New("MekBay has no such operation any more")
	}
	var out BattleStatus
	for _, f := range op.Data.Forces {
		if f.Alignment != "friendly" {
			continue
		}
		got, err := s.Call(ctx, getForceMessage(s.uuid, f.InstanceID, false))
		if err != nil {
			return BattleStatus{}, err
		}
		h, err := ParseHangarForce(got)
		if err != nil {
			continue // a force deleted from MekBay since: the others are still worth showing
		}
		out.Forces = append(out.Forces, BattleForce{InstanceID: f.InstanceID, Alignment: f.Alignment, Force: h})
	}
	if len(out.Forces) == 0 {
		return BattleStatus{}, errors.New("none of the players' forces could be read from MekBay")
	}
	return out, nil
}

// DeleteBattle deletes a battle's operation and every force in it (the players' forces and the OpFor) in MekBay, best effort: MekBay does not acknowledge a
// deletion. It is how an aborted battle is cleaned up. It reads the operation first, so it works whatever the site still remembers of it.
func (c *Client) DeleteBattle(ctx context.Context, owner, operationID string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := Dial(ctx, c.url(), owner)
	if err != nil {
		return err
	}
	defer s.Close()
	raw, err := s.Call(ctx, map[string]any{"action": "getOperation", "operationId": operationID})
	if err != nil {
		return err
	}
	var op struct {
		Data *struct {
			Forces []struct {
				InstanceID string `json:"instanceId"`
			} `json:"forces"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &op); err != nil || op.Data == nil {
		return nil // already gone
	}
	for _, f := range op.Data.Forces {
		if err := s.Send(ctx, delForceMessage(owner, f.InstanceID)); err != nil {
			return err
		}
	}
	if err := s.Send(ctx, map[string]any{"action": "delOperation", "operationId": operationID}); err != nil {
		return err
	}
	_, _ = s.Call(ctx, map[string]any{"action": "getOperation", "operationId": operationID}) // after this the deletions have been handled
	return nil
}
