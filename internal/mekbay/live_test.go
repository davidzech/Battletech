package mekbay

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestStartBattleLive starts a battle on the real mekbay.com with a throwaway account (a random UUID, tiny forces) and checks what was saved, starting twice to
// see the operation updated in place. It runs only when MEKBAY_LIVE_BATTLE is set.
func TestStartBattleLive(t *testing.T) {
	if os.Getenv("MEKBAY_LIVE_BATTLE") == "" {
		t.Skip("set MEKBAY_LIVE_BATTLE=1 to start a battle on mekbay.com")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	account := NewID()

	// A hangar with three mechs, owned by the same account (as the site's hangar copy is).
	hs, err := Dial(ctx, mekbayWS, account)
	if err != nil {
		t.Fatal(err)
	}
	defer hs.Close()
	hangarID := NewID()
	hangar := BuildForce(hangarID, time.Now().UTC(), ForceDraft{Name: "Live hangar", Groups: []GroupDraft{{Name: "Hangar", Units: []UnitDraft{{Unit: "BMBlackHawk_Prime"}, {Unit: "BMShadowCat_Prime"}, {Unit: "BMRyoken_Prime"}}}}})
	if _, err := hs.Call(ctx, map[string]any{"action": "saveForce", "uuid": account, "data": hangar, "savedForceCount": 1}); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, u := range hangar.Groups[0].Units {
		ids = append(ids, u.ID)
	}

	cl := &Client{}
	req := BattleRequest{
		Account:        account,
		HangarInstance: hangarID,
		OperationName:  "Live test - Scenario",
		OpFor:          BattleOpFor{Draft: ForceDraft{Name: "Live OpFor", Groups: []GroupDraft{{Name: "OpFor 1", Units: []UnitDraft{{Unit: "BMLoki_Prime", Pilot: "Alrek", Gunnery: 3, Piloting: 4}}}}}},
		Players: []BattlePlayer{
			{Number: 1, Name: "Live - Player 1", Units: []string{ids[0], ids[1]}, Crew: []CrewAssignment{{UnitID: ids[0], Name: "Katarina", Gunnery: 3, Piloting: 4}}},
			{Number: 2, Name: "Live - Player 2", Units: []string{ids[2]}},
		},
	}
	res, err := cl.StartBattle(ctx, req)
	if err != nil {
		t.Fatalf("start battle: %v", err)
	}
	t.Logf("operation %s, opfor %s, players %v", res.OperationID, res.OpForInstance, res.PlayerInstances)

	// The forces read back as built.
	for i, want := range []int{2, 1} {
		h, err := cl.ReadHangar(ctx, res.PlayerInstances[i])
		if err != nil || len(h.Units) != want {
			t.Errorf("player %d's force: %d units (want %d) %v", i+1, len(h.Units), want, err)
		}
	}
	if h, _ := cl.ReadHangar(ctx, res.PlayerInstances[0]); len(h.Units) == 2 && h.Units[0].Pilot != "Katarina" {
		t.Errorf("the crew assigned to Player 1's first mech: %q", h.Units[0].Pilot)
	}

	// The operation lists both players' forces as friendly and the OpFor as the enemy.
	forces := func(operation string) map[string]string {
		raw, err := hs.Call(ctx, map[string]any{"action": "getOperation", "operationId": operation})
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Data struct {
				Forces []struct {
					InstanceID string `json:"instanceId"`
					Alignment  string `json:"alignment"`
				} `json:"forces"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("%v: %s", err, raw)
		}
		out := map[string]string{}
		for _, f := range got.Data.Forces {
			out[f.InstanceID] = f.Alignment
		}
		return out
	}
	got := forces(res.OperationID)
	if len(got) != 3 || got[res.PlayerInstances[0]] != "friendly" || got[res.PlayerInstances[1]] != "friendly" || got[res.OpForInstance] != "enemy" {
		t.Errorf("the operation: %v", got)
	}

	// Starting again updates the same operation with the new forces and keeps the OpFor.
	req.OperationID, req.OpFor.Reuse = res.OperationID, res.OpForInstance
	again, err := cl.StartBattle(ctx, req)
	if err != nil {
		t.Fatalf("starting again: %v", err)
	}
	if again.OperationID != res.OperationID || again.OpForInstance != res.OpForInstance {
		t.Errorf("the same operation and OpFor: %+v vs %+v", again, res)
	}
	got = forces(again.OperationID)
	if len(got) != 3 || got[again.PlayerInstances[0]] != "friendly" || got[again.OpForInstance] != "enemy" || got[res.PlayerInstances[0]] != "" {
		t.Errorf("the operation now holds the new forces: %v", got)
	}
}

// TestCommitBattleLive writes a battle back into a hangar on the real mekbay.com with a throwaway account. It runs only when MEKBAY_LIVE_BATTLE is set.
func TestCommitBattleLive(t *testing.T) {
	if os.Getenv("MEKBAY_LIVE_BATTLE") == "" {
		t.Skip("set MEKBAY_LIVE_BATTLE=1 to run against mekbay.com")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	account := NewID()
	hs, err := Dial(ctx, mekbayWS, account)
	if err != nil {
		t.Fatal(err)
	}
	defer hs.Close()
	hangarID := NewID()
	hangar := BuildForce(hangarID, time.Now().UTC(), ForceDraft{Name: "Live hangar", Groups: []GroupDraft{{Name: "Hangar", Units: []UnitDraft{{Unit: "BMBlackHawk_Prime"}, {Unit: "BMBlackHawk_Prime"}}}}})
	if _, err := hs.Call(ctx, map[string]any{"action": "saveForce", "uuid": account, "data": hangar, "savedForceCount": 1}); err != nil {
		t.Fatal(err)
	}
	ids := []string{hangar.Groups[0].Units[0].ID, hangar.Groups[0].Units[1].ID}
	cl := &Client{}
	res, err := cl.StartBattle(ctx, BattleRequest{Account: account, HangarInstance: hangarID, OperationName: "Live commit", OpFor: BattleOpFor{Draft: ForceDraft{Name: "OpFor", Groups: []GroupDraft{{Name: "OpFor 1", Units: []UnitDraft{{Unit: "BMLoki_Prime"}}}}}},
		Players: []BattlePlayer{{Number: 1, Name: "P1", Units: []string{ids[1]}, Crew: []CrewAssignment{{UnitID: ids[1], Name: "Katarina", Gunnery: 3, Piloting: 4}}}}})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := cl.CommitBattle(ctx, CommitBattleRequest{OwnerUID: account, HangarInstance: hangarID, OperationID: res.OperationID})
	if err != nil || rep.Matched != 1 {
		t.Fatalf("commit: %+v %v", rep, err)
	}
	h, err := cl.ReadHangar(ctx, hangarID)
	if err != nil || len(h.Units) != 2 || h.Units[0].Pilot != "" && h.Units[0].Pilot != "Pilot" {
		t.Errorf("the hangar after: %+v %v", h.Units, err)
	}
	t.Logf("hangar crew after: %q / %q", h.Units[0].Pilot, h.Units[1].Pilot)
}

// TestCheckpointForcesLive copies a force, deletes forces and deletes a battle on the real mekbay.com with a throwaway account: what checkpoints and aborts
// rely on. It runs only when MEKBAY_LIVE_BATTLE is set.
func TestCheckpointForcesLive(t *testing.T) {
	if os.Getenv("MEKBAY_LIVE_BATTLE") == "" {
		t.Skip("set MEKBAY_LIVE_BATTLE=1 to run against mekbay.com")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	account := NewID()
	hs, err := Dial(ctx, mekbayWS, account)
	if err != nil {
		t.Fatal(err)
	}
	defer hs.Close()
	hangarID := NewID()
	hangar := BuildForce(hangarID, time.Now().UTC(), ForceDraft{Name: "Live hangar", Groups: []GroupDraft{{Name: "Hangar", Units: []UnitDraft{{Unit: "BMBlackHawk_Prime"}}}}})
	if err := hs.saveData(ctx, hangar); err != nil {
		t.Fatal(err)
	}
	cl := &Client{}
	exists := func(instance string) bool {
		f, err := hs.getForce(ctx, instance, true)
		if err != nil {
			t.Fatal(err)
		}
		return f != nil
	}

	// A copy is a force of its own, with the same units.
	copyID := NewID()
	if err := cl.CopyForce(ctx, CopyRequest{OwnerUID: account, Source: hangarID, Target: copyID}); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if h, err := cl.ReadHangar(ctx, copyID); err != nil || len(h.Units) != 1 || h.Units[0].Unit != "BMBlackHawk_Prime" {
		t.Fatalf("the copy: %+v %v", h, err)
	}
	// Copying it back over the original works too (a rollback).
	if err := cl.CopyForce(ctx, CopyRequest{OwnerUID: account, Source: copyID, Target: hangarID}); err != nil {
		t.Fatalf("copy back: %v", err)
	}
	// Deleting it removes it (MekBay doesn't acknowledge, so DeleteForce waits for a read on the same connection).
	if err := cl.DeleteForce(ctx, account, copyID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if exists(copyID) {
		t.Error("the deleted copy is gone")
	}
	if !exists(hangarID) {
		t.Error("the hangar is still there")
	}

	// A battle's operation and every force in it go with DeleteBattle.
	res, err := cl.StartBattle(ctx, BattleRequest{Account: account, HangarInstance: hangarID, OperationName: "Live delete", OpFor: BattleOpFor{Draft: ForceDraft{Name: "OpFor", Groups: []GroupDraft{{Name: "OpFor 1", Units: []UnitDraft{{Unit: "BMLoki_Prime"}}}}}},
		Players: []BattlePlayer{{Number: 1, Name: "P1", Units: []string{hangar.Groups[0].Units[0].ID}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := cl.DeleteBattle(ctx, account, res.OperationID); err != nil {
		t.Fatalf("delete battle: %v", err)
	}
	for _, in := range append([]string{res.OpForInstance}, res.PlayerInstances...) {
		if exists(in) {
			t.Errorf("force %s of the battle is gone", in)
		}
	}
	if _, err := cl.ReadBattle(ctx, res.OperationID); err == nil {
		t.Error("the operation is gone")
	}
	if !exists(hangarID) {
		t.Error("the hangar is untouched")
	}
}
