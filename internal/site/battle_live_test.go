package site

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
	account := newID()

	// A hangar with three mechs, owned by the same account (as the site's hangar copy is).
	hs, err := dialMekBay(ctx, mekbayWSURL, account)
	if err != nil {
		t.Fatal(err)
	}
	defer hs.close()
	hangarID := newID()
	hangar := buildForce(hangarID, time.Now().UTC(), ForceDraft{Name: "Live hangar", Groups: []GroupDraft{{Name: "Hangar", Units: []UnitDraft{{Unit: "BMBlackHawk_Prime"}, {Unit: "BMShadowCat_Prime"}, {Unit: "BMRyoken_Prime"}}}}})
	if _, err := hs.call(ctx, map[string]any{"action": "saveForce", "uuid": account, "data": hangar, "savedForceCount": 1}); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, u := range hangar.Groups[0].Units {
		ids = append(ids, u.ID)
	}

	cl := &MekBayClient{}
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
		raw, err := hs.call(ctx, map[string]any{"action": "getOperation", "operationId": operation})
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
