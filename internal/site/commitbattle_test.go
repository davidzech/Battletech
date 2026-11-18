package site

import (
	"context"
	"strings"
	"testing"
	"time"
)

func hangarWith(units ...map[string]any) map[string]any {
	us := make([]any, len(units))
	for i, u := range units {
		us[i] = u
	}
	return map[string]any{"instanceId": "H", "name": "Hangar", "groups": []any{map[string]any{"id": "g", "name": "Hangar", "units": us}}}
}

func unitMap(id, design string, state map[string]any) map[string]any {
	return map[string]any{"id": id, "unit": design, "state": state}
}

func stateOf(h map[string]any, id string) map[string]any {
	for _, g := range h["groups"].([]any) {
		for _, u := range g.(map[string]any)["units"].([]any) {
			if um := u.(map[string]any); um["id"] == id {
				s, _ := um["state"].(map[string]any)
				return s
			}
		}
	}
	return nil
}

// Two of the same design in the hangar are told apart by their IDs: the battle's unit goes back into the one it was copied from, and only that one. The crew never
// goes back, and a crew the hangar had is cleared, because the hangar holds no pilots.
func TestCommitStateMatchesByID(t *testing.T) {
	hangar := hangarWith(
		unitMap("a1", "BMOrion_Prime", map[string]any{"crew": []any{map[string]any{"name": "Alrek"}}}),
		unitMap("a2", "BMOrion_Prime", nil),
		unitMap("b1", "BMGrizzly", map[string]any{"locations": map[string]any{"CT": map[string]any{"armor": float64(3)}}}),
	)
	battle := map[string]map[string]any{
		"a2": unitMap("a2", "BMOrion_Prime", map[string]any{"locations": map[string]any{"LT": map[string]any{"armor": float64(9)}}, "crew": []any{map[string]any{"name": "Katarina", "hits": float64(6)}}}),
		"zz": unitMap("zz", "BMLoki_Prime", map[string]any{}),              // not in the hangar any more
		"b1": unitMap("b1", "BMMadCat_H", map[string]any{"x": float64(1)}), // the ID matches but the design doesn't: not the same unit
	}
	rep := commitState(hangar, battle)
	if rep.Matched != 1 || len(rep.Skipped) != 2 {
		t.Fatalf("one unit written back, two skipped: %+v", rep)
	}
	if got := stateOf(hangar, "a2"); got == nil || got["locations"] == nil || got["crew"] != nil {
		t.Errorf("the second Orion has the battle's damage and no crew: %+v", got)
	}
	if got := stateOf(hangar, "a1"); got == nil || got["crew"] == nil {
		t.Errorf("the first Orion was not in the battle and is left exactly as it was: %+v", got)
	}
	if got := stateOf(hangar, "b1"); got["x"] != nil || got["locations"] == nil {
		t.Errorf("a different design with the same ID is left alone: %+v", got)
	}
	// The same state written twice changes nothing.
	again := commitState(hangar, battle)
	if again.Matched != 1 || stateOf(hangar, "a2")["crew"] != nil {
		t.Errorf("writing back twice: %+v", again)
	}
}

// Through the WebSocket: the hangar's units take the damage their battle copies recorded, by ID, and the hangar ends with no crew.
func TestCommitBattleWritesTheHangar(t *testing.T) {
	f := newFakeForceServer(t)
	cl := &MekBayClient{URL: f.wsURL()}
	hangar, ids := f.saveHangar(tableAccount, ForceDraft{Name: "Hangar", Groups: []GroupDraft{{Name: "G", Units: []UnitDraft{{Unit: "BMOrion_Prime"}, {Unit: "BMOrion_Prime"}, {Unit: "BMGrizzly"}}}}})
	f.mu.Lock()
	copyOf, err := subsetForce(f.forces[hangar], []string{ids[1]}, "P1", "Player 1", time.Now())
	f.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	unit := copyOf["groups"].([]any)[0].(map[string]any)["units"].([]any)[0].(map[string]any)
	state, _ := unit["state"].(map[string]any)
	if state == nil {
		state = map[string]any{}
		unit["state"] = state
	}
	state["locations"] = map[string]any{"CT": map[string]any{"armor": float64(12)}}
	state["crew"] = []any{map[string]any{"name": "Alrek", "gunnerySkill": float64(3), "pilotingSkill": float64(4), "hits": float64(1)}}
	f.mu.Lock()
	f.forces["P1"], f.owners["P1"] = copyOf, tableAccount
	f.ops["op1"] = map[string]any{"name": "S", "forces": []any{map[string]any{"instanceId": "P1", "alignment": "friendly"}, map[string]any{"instanceId": "OPFOR", "alignment": "enemy"}}}
	f.mu.Unlock()

	rep, err := cl.CommitBattle(context.Background(), CommitBattleRequest{OwnerUID: tableAccount, HangarInstance: hangar, OperationID: "op1"})
	if err != nil || rep.Matched != 1 || len(rep.Skipped) != 0 {
		t.Fatalf("commit: %+v %v", rep, err)
	}
	f.mu.Lock()
	got := f.forces[hangar]
	other := stateOf(got, ids[0])
	fought := stateOf(got, ids[1])
	f.mu.Unlock()
	if s := fought; s == nil || len(s["locations"].(map[string]any)) == 0 || s["crew"] != nil {
		t.Errorf("the unit that fought has its damage and no crew: %+v", s)
	}
	if s := other; s != nil && len(s["locations"].(map[string]any)) != 0 {
		t.Errorf("the other Orion, which stayed home, is untouched: %+v", s)
	}
	if _, err := cl.CommitBattle(context.Background(), CommitBattleRequest{OwnerUID: tableAccount, HangarInstance: hangar, OperationID: "nope"}); err == nil || !strings.Contains(err.Error(), "no such operation") {
		t.Errorf("an operation that is not there: %v", err)
	}
}

// The MVP earns an extra mvpBonusSP, on top of their share; only one pilot can be it, and only one who took part and lives.
func TestMVPBonus(t *testing.T) {
	sc := acesScenario(t, 80)
	mission := map[string]any{"depot": true, "escape": false}
	run := func(entries ...OutcomeEntry) (OutcomeResult, error) {
		return sc.EvaluateOutcome(OutcomeInputs{Pilots: entries, Mission: mission}, map[string]string{"a": "Alrek", "b": "Katarina", "c": "Jarek", "d": "Sybille"})
	}
	res, err := run(OutcomeEntry{PilotID: "a", TookPart: true, MVP: true}, OutcomeEntry{PilotID: "b", TookPart: true})
	if err != nil || sps(res) != "Alrek=100,Katarina=80" || !res.Pilots[0].MVP || res.Pool != 400 || res.Warchest != 240 {
		t.Errorf("the MVP gets 20 on top of 80, and it does not come out of the pool: %+v %v (%s)", res, err, sps(res))
	}
	for name, entries := range map[string][]OutcomeEntry{
		"two":     {{PilotID: "a", TookPart: true, MVP: true}, {PilotID: "b", TookPart: true, MVP: true}},
		"benched": {{PilotID: "a", TookPart: false, MVP: true}},
		"killed":  {{PilotID: "a", TookPart: true, Killed: true, MVP: true}},
	} {
		if _, err := run(entries...); err == nil {
			t.Errorf("%s: an MVP who cannot be one is refused", name)
		}
	}
}
