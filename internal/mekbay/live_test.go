package mekbay

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// liveHangar is a hangar built the way the site stores one (never saved in MekBay), and its units' IDs.
func liveHangar(t *testing.T, units ...string) ([]byte, []string) {
	t.Helper()
	var drafts []UnitDraft
	for _, u := range units {
		drafts = append(drafts, UnitDraft{Unit: u})
	}
	hangar := BuildForce(NewID(), time.Now().UTC(), ForceDraft{Name: "Live hangar", Groups: []GroupDraft{{Name: "Hangar", Units: drafts}}})
	var ids []string
	for _, u := range hangar.Groups[0].Units {
		ids = append(ids, u.ID)
	}
	b, err := json.Marshal(hangar)
	if err != nil {
		t.Fatal(err)
	}
	return b, ids
}

// readLive reads a force from mekbay.com as the site reads one.
func readLive(ctx context.Context, cl *Client, instance string) (HangarForce, error) {
	raw, err := cl.ReadForce(ctx, instance)
	if err != nil {
		return HangarForce{}, err
	}
	return ParseForce(raw)
}

// TestStartBattleLive starts a battle on the real mekbay.com with a throwaway account (a random UUID, tiny forces) and checks what was saved, starting twice to
// see the operation updated in place. It runs only when MEKBAY_LIVE_BATTLE is set.
func TestStartBattleLive(t *testing.T) {
	if os.Getenv("MEKBAY_LIVE_BATTLE") == "" {
		t.Skip("set MEKBAY_LIVE_BATTLE=1 to start a battle on mekbay.com")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	account := NewID()
	hs, err := Dial(ctx, mekbayWS, account)
	if err != nil {
		t.Fatal(err)
	}
	defer hs.Close()
	hangar, ids := liveHangar(t, "BMBlackHawk_Prime", "BMShadowCat_Prime", "BMRyoken_Prime")

	cl := &Client{}
	req := BattleRequest{
		Account:       account,
		Hangar:        hangar,
		OperationName: "Live test - Scenario",
		OpFor:         BattleOpFor{Draft: ForceDraft{Name: "Live OpFor", Groups: []GroupDraft{{Name: "OpFor 1", Units: []UnitDraft{{Unit: "BMLoki_Prime", Pilot: "Alrek", Gunnery: 3, Piloting: 4}}}}}},
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
	defer func() { _ = cl.DeleteBattle(context.Background(), account, res.OperationID) }()

	// The forces read back as built.
	for i, want := range []int{2, 1} {
		h, err := readLive(ctx, cl, res.PlayerInstances[i])
		if err != nil || len(h.Units) != want {
			t.Errorf("player %d's force: %d units (want %d) %v", i+1, len(h.Units), want, err)
		}
	}
	if h, _ := readLive(ctx, cl, res.PlayerInstances[0]); len(h.Units) == 2 && h.Units[0].Pilot != "Katarina" {
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

// TestCommitBattleLive reads a battle's units back from the real mekbay.com with a throwaway account and writes them into a hangar as the site does. It runs only
// when MEKBAY_LIVE_BATTLE is set.
func TestCommitBattleLive(t *testing.T) {
	if os.Getenv("MEKBAY_LIVE_BATTLE") == "" {
		t.Skip("set MEKBAY_LIVE_BATTLE=1 to run against mekbay.com")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	account := NewID()
	hangar, ids := liveHangar(t, "BMBlackHawk_Prime", "BMBlackHawk_Prime")
	cl := &Client{}
	res, err := cl.StartBattle(ctx, BattleRequest{Account: account, Hangar: hangar, OperationName: "Live commit", OpFor: BattleOpFor{Draft: ForceDraft{Name: "OpFor", Groups: []GroupDraft{{Name: "OpFor 1", Units: []UnitDraft{{Unit: "BMLoki_Prime"}}}}}},
		Players: []BattlePlayer{{Number: 1, Name: "P1", Units: []string{ids[1]}, Crew: []CrewAssignment{{UnitID: ids[1], Name: "Katarina", Gunnery: 3, Piloting: 4}}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cl.DeleteBattle(context.Background(), account, res.OperationID) }()
	battle, err := cl.ReadBattleUnits(ctx, account, res.OperationID, nil)
	if err != nil {
		t.Fatal(err)
	}
	var h map[string]any
	if err := json.Unmarshal(hangar, &h); err != nil {
		t.Fatal(err)
	}
	if rep := CommitState(h, battle); rep.Matched != 1 {
		t.Fatalf("commit: %+v", rep)
	}
	b, _ := json.Marshal(h)
	after, err := ParseForce(b)
	if err != nil || len(after.Units) != 2 || after.Units[1].Pilot != "" {
		t.Errorf("the hangar after: %+v %v", after.Units, err)
	}
}

// TestDeleteBattleLive deletes a battle on the real mekbay.com with a throwaway account: what aborts and completions rely on. It runs only when
// MEKBAY_LIVE_BATTLE is set.
func TestDeleteBattleLive(t *testing.T) {
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
	hangar, ids := liveHangar(t, "BMBlackHawk_Prime")
	cl := &Client{}
	exists := func(instance string) bool {
		f, err := hs.getForce(ctx, instance, true)
		if err != nil {
			t.Fatal(err)
		}
		return f != nil
	}

	// A battle's operation and every force in it go with DeleteBattle.
	res, err := cl.StartBattle(ctx, BattleRequest{Account: account, Hangar: hangar, OperationName: "Live delete", OpFor: BattleOpFor{Draft: ForceDraft{Name: "OpFor", Groups: []GroupDraft{{Name: "OpFor 1", Units: []UnitDraft{{Unit: "BMLoki_Prime"}}}}}},
		Players: []BattlePlayer{{Number: 1, Name: "P1", Units: []string{ids[0]}}}})
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
}
