package mekbay

import (
	"context"
	"encoding/json"
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

// crewIsUnnamed says whether a unit state's crew is the default one: present, with no name and a 4/5 pilot.
func crewIsUnnamed(state map[string]any) bool {
	crew, _ := state["crew"].([]any)
	if len(crew) == 0 {
		return false
	}
	for _, c := range crew {
		cm, _ := c.(map[string]any)
		if cm["name"] != "" || num(cm["gunnerySkill"]) != DefaultGunnery || num(cm["pilotingSkill"]) != DefaultPiloting || num(cm["hits"]) != 0 {
			return false
		}
	}
	return true
}

// num reads a JSON-ish number, whichever Go type it has (a force decoded from JSON has float64s; one changed in Go may have ints).
func num(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	}
	return -1
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
	rep := CommitState(hangar, battle)
	if rep.Matched != 1 || len(rep.Skipped) != 2 {
		t.Fatalf("one unit written back, two skipped: %+v", rep)
	}
	if got := stateOf(hangar, "a2"); got == nil || got["locations"] == nil || !crewIsUnnamed(got) {
		t.Errorf("the second Orion has the battle's damage and an unnamed crew: %+v", got)
	}
	if got := stateOf(hangar, "a1"); got == nil || got["crew"] == nil {
		t.Errorf("the first Orion was not in the battle and is left exactly as it was: %+v", got)
	}
	if got := stateOf(hangar, "b1"); got["x"] != nil || got["locations"] == nil {
		t.Errorf("a different design with the same ID is left alone: %+v", got)
	}
	// The same state written twice changes nothing.
	again := CommitState(hangar, battle)
	if again.Matched != 1 || !crewIsUnnamed(stateOf(hangar, "a2")) {
		t.Errorf("writing back twice: %+v", again)
	}
}

// hangarCopy is a deep copy of a force the fake server holds, as the site would load the hangar from its database.
func hangarCopy(t *testing.T, f *fakeForceServer, instance string) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := deepCopy(f.forces[instance])
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCommitBattleWritesTheHangar(t *testing.T) {
	f := newFakeForceServer(t)
	cl := &Client{URL: f.wsURL()}
	hangar, ids := f.saveHangar(tableAccount, ForceDraft{Name: "Hangar", Groups: []GroupDraft{{Name: "G", Units: []UnitDraft{{Unit: "BMOrion_Prime"}, {Unit: "BMOrion_Prime"}, {Unit: "BMGrizzly"}}}}})
	h := hangarCopy(t, f, hangar)
	copyOf, err := subsetForce(h, []string{ids[1]}, "P1", "Player 1", time.Now())
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

	battle, err := cl.ReadBattleUnits(context.Background(), tableAccount, "op1", nil)
	if err != nil || len(battle) != 1 {
		t.Fatalf("read the battle: %v %v", battle, err)
	}
	if rep := CommitState(h, battle); rep.Matched != 1 || len(rep.Skipped) != 0 {
		t.Fatalf("commit: %+v", rep)
	}
	if s := stateOf(h, ids[1]); s == nil || len(s["locations"].(map[string]any)) == 0 || !crewIsUnnamed(s) {
		t.Errorf("the unit that fought has its damage and no crew: %+v", s)
	}
	if s := stateOf(h, ids[0]); s != nil && len(s["locations"].(map[string]any)) != 0 {
		t.Errorf("the other Orion, which stayed home, is untouched: %+v", s)
	}
	b, _ := json.Marshal(h)
	if saved, err := ParseForce(b); err != nil || len(saved.Units) != 3 || saved.Units[1].ID != ids[1] || saved.Units[1].Locations["CT"].Armor != 12 || saved.Units[0].Damaged {
		t.Errorf("the hangar as the site reads it: %+v %v", saved.Units, err)
	}
	if _, err := cl.ReadBattleUnits(context.Background(), tableAccount, "nope", nil); err == nil || !strings.Contains(err.Error(), "no such operation") {
		t.Errorf("an operation that is not there: %v", err)
	}
}

// The scenario's allies are a friendly force of the operation, but their units are never the players' mechs: the write-back leaves out a force it is told to skip.
func TestCommitBattleSkipsTheAllies(t *testing.T) {
	f := newFakeForceServer(t)
	cl := &Client{URL: f.wsURL()}
	hangar, ids := f.saveHangar(tableAccount, ForceDraft{Name: "Hangar", Groups: []GroupDraft{{Name: "G", Units: []UnitDraft{{Unit: "BMGrizzly"}}}}})
	h := hangarCopy(t, f, hangar)
	p1, err := subsetForce(h, []string{ids[0]}, "P1", "Player 1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	allies := BuildForce("ALLIES", time.Now(), ForceDraft{Name: "Allies", Groups: []GroupDraft{{Name: "Allies", Units: []UnitDraft{{Unit: "BMKoshi_C"}, {Unit: "SVSaviorRepairVehicle"}}}}})
	b, _ := json.Marshal(allies)
	var am map[string]any
	_ = json.Unmarshal(b, &am)
	f.mu.Lock()
	f.forces["P1"], f.owners["P1"] = p1, tableAccount
	f.forces["ALLIES"], f.owners["ALLIES"] = am, tableAccount
	f.ops["op1"] = map[string]any{"name": "S", "forces": []any{
		map[string]any{"instanceId": "P1", "alignment": "friendly"}, map[string]any{"instanceId": "ALLIES", "alignment": "friendly"}, map[string]any{"instanceId": "OPFOR", "alignment": "enemy"}}}
	f.mu.Unlock()

	battle, err := cl.ReadBattleUnits(context.Background(), tableAccount, "op1", nil)
	if rep := CommitState(h, battle); err != nil || rep.Matched != 1 || len(rep.Skipped) != 2 {
		t.Fatalf("without skip the allies' units are read and find no hangar unit: %+v %v", rep, err)
	}
	battle, err = cl.ReadBattleUnits(context.Background(), tableAccount, "op1", []string{"ALLIES"})
	if rep := CommitState(h, battle); err != nil || rep.Matched != 1 || len(rep.Skipped) != 0 {
		t.Fatalf("with skip the allies' force is left out: %+v %v", rep, err)
	}
}

// A battle force MekBay fails to read fails the read (the player tries again); a battle none of whose units are in the hangar changes nothing.
func TestCommitBattleReadErrors(t *testing.T) {
	f := newFakeForceServer(t)
	cl := &Client{URL: f.wsURL()}
	hangar, ids := f.saveHangar(tableAccount, ForceDraft{Name: "Hangar", Groups: []GroupDraft{{Name: "G", Units: []UnitDraft{{Unit: "BMGrizzly"}}}}})
	h := hangarCopy(t, f, hangar)
	p1, err := subsetForce(h, []string{ids[0]}, "P1", "Player 1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.forces["P1"], f.owners["P1"] = p1, tableAccount
	f.ops["op1"] = map[string]any{"name": "S", "forces": []any{map[string]any{"instanceId": "P1", "alignment": "friendly"}}}
	f.failRead = "P1"
	f.mu.Unlock()
	if _, err := cl.ReadBattleUnits(context.Background(), tableAccount, "op1", nil); err == nil {
		t.Error("a battle force MekBay failed to read fails the read")
	}

	f.mu.Lock()
	f.failRead = ""
	f.forces["P1"]["groups"].([]any)[0].(map[string]any)["units"].([]any)[0].(map[string]any)["id"] = "elsewhere"
	f.mu.Unlock()
	before, _ := json.Marshal(h)
	battle, err := cl.ReadBattleUnits(context.Background(), tableAccount, "op1", nil)
	rep := CommitState(h, battle)
	after, _ := json.Marshal(h)
	if err != nil || rep.Matched != 0 || len(rep.Skipped) != 1 || string(before) != string(after) {
		t.Errorf("a battle with no unit from the hangar: %+v %v", rep, err)
	}
}
