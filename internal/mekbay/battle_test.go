package mekbay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeForceServer is a stand-in for mekbay.com's WebSocket server that keeps forces and operations the way the real one does: a force belongs to the account
// that saved it and keeps the instance ID it was saved under, and an operation is saved without an acknowledgement and read back with getOperation.
type fakeForceServer struct {
	*httptest.Server
	mu     sync.Mutex
	forces map[string]map[string]any // instance -> saved data
	owners map[string]string         // instance -> account
	ops    map[string]map[string]any // operation id -> saved data
	// refuse is the name of a force the server refuses to save (none when ""); saves counts the forces it did save.
	refuse string
	saves  int
	// failRead is the instance of a force the server fails to read, as on a transient error (none when "").
	failRead string
}

func newFakeForceServer(t *testing.T) *fakeForceServer {
	t.Helper()
	f := &fakeForceServer{forces: map[string]map[string]any{}, owners: map[string]string{}, ops: map[string]map[string]any{}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeForceServer) wsURL() string { return "ws" + strings.TrimPrefix(f.URL, "http") }

func (f *fakeForceServer) serve(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx := r.Context()
	account := ""
	for {
		_, b, err := c.Read(ctx)
		if err != nil {
			return
		}
		var m map[string]any
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		rid, _ := m["requestId"].(string)
		reply := func(v map[string]any) {
			v["requestId"] = rid
			out, _ := json.Marshal(v)
			_ = c.Write(ctx, websocket.MessageText, out)
		}
		f.mu.Lock()
		switch m["action"] {
		case "register":
			account, _ = m["uuid"].(string)
		case "saveForce":
			data, _ := m["data"].(map[string]any)
			inst, _ := data["instanceId"].(string)
			if owner, ok := f.owners[inst]; ok && owner != account {
				reply(map[string]any{"action": "error", "code": "not_owner", "message": "not the owner"})
				break
			}
			if name, _ := data["name"].(string); f.refuse != "" && name == f.refuse {
				reply(map[string]any{"action": "error", "message": "refused"})
				break
			}
			f.forces[inst], f.owners[inst] = data, account
			f.saves++
			reply(map[string]any{"action": "forceSaved", "instanceId": inst})
		case "delForce": // not acknowledged, like the real one
			inst, _ := m["instanceId"].(string)
			if f.owners[inst] == account {
				delete(f.forces, inst)
				delete(f.owners, inst)
			}
		case "delOperation":
			id, _ := m["operationId"].(string)
			delete(f.ops, id)
		case "getForce":
			inst, _ := m["instanceId"].(string)
			owned, _ := m["ownedOnly"].(bool)
			if f.failRead != "" && inst == f.failRead {
				reply(map[string]any{"action": "error", "message": "timed out"})
				break
			}
			var data any
			if d, ok := f.forces[inst]; ok && (!owned || f.owners[inst] == account) {
				data = d
			}
			reply(map[string]any{"action": "forceData", "data": data})
		case "saveOperation":
			data, _ := m["data"].(map[string]any)
			id, _ := data["operationId"].(string)
			f.ops[id] = data
		case "getOperation":
			id, _ := m["operationId"].(string)
			var data any
			if d, ok := f.ops[id]; ok {
				data = map[string]any{"owned": true, "operationId": id, "forces": d["forces"], "name": d["name"]}
			}
			reply(map[string]any{"action": "operationData", "data": data})
		}
		f.mu.Unlock()
	}
}

func (f *fakeForceServer) saveHangar(owner string, draft ForceDraft) (instance string, unitIDs []string) {
	instance = NewID()
	b, _ := json.Marshal(BuildForce(instance, time.Now().UTC(), draft))
	var data map[string]any
	_ = json.Unmarshal(b, &data)
	f.forces[instance], f.owners[instance] = data, owner
	for _, g := range data["groups"].([]any) {
		for _, u := range g.(map[string]any)["units"].([]any) {
			unitIDs = append(unitIDs, u.(map[string]any)["id"].(string))
		}
	}
	return instance, unitIDs
}

// hangarJSON is a force the server holds as the site stores a hangar: its saved data as JSON.
func (f *fakeForceServer) hangarJSON(instance string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, _ := json.Marshal(f.forces[instance])
	return b
}

func unitsOf(data map[string]any) (names, crews []string) {
	for _, g := range data["groups"].([]any) {
		for _, u := range g.(map[string]any)["units"].([]any) {
			um := u.(map[string]any)
			names = append(names, um["unit"].(string))
			crew := um["state"].(map[string]any)["crew"].([]any)
			crews = append(crews, crew[0].(map[string]any)["name"].(string))
		}
	}
	return
}

// operationForces lists an operation's forces as "alignment:instance".
func operationForces(op map[string]any) []string {
	var out []string
	for _, f := range op["forces"].([]any) {
		fm := f.(map[string]any)
		out = append(out, fm["alignment"].(string)+":"+fm["instanceId"].(string))
	}
	return out
}

const tableAccount = "00000000-0000-4000-8000-0000000000a0"

func TestStartBattleBuildsForcesAndTheOperation(t *testing.T) {
	f := newFakeForceServer(t)
	cl := &Client{URL: f.wsURL()}
	f.mu.Lock()
	hangar, ids := f.saveHangar(tableAccount, ForceDraft{Name: "Hangar", Groups: []GroupDraft{{Name: "G", Units: []UnitDraft{{Unit: "BMMadCat_H"}, {Unit: "BMKoshi_E"}, {Unit: "BMGrizzly"}}}}})
	f.mu.Unlock()
	req := BattleRequest{
		Account:       tableAccount,
		Hangar:        f.hangarJSON(hangar),
		OperationName: "Refusal War - Scenario 001",
		OpFor:         BattleOpFor{Draft: ForceDraft{Name: "OpFor", Groups: []GroupDraft{{Name: "OpFor 1", Units: []UnitDraft{{Unit: "BMLoki_Prime", Pilot: "Alrek", Gunnery: 3, Piloting: 4}}}}}},
		Players: []BattlePlayer{
			{Number: 1, Name: "Dragoons - Player 1", Units: []string{ids[0], ids[1]}, Crew: []CrewAssignment{{UnitID: ids[0], Name: "Katarina", Gunnery: 3, Piloting: 4}}},
			{Number: 2, Name: "Dragoons - Player 2", Units: []string{ids[2]}},
		},
	}
	res, err := cl.StartBattle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if res.OperationID == "" || res.OpForInstance == "" || len(res.PlayerInstances) != 2 {
		t.Fatalf("the result: %+v", res)
	}
	// Every force is saved under the seed's one account, with the right units, crews and names.
	for _, inst := range append([]string{res.OpForInstance}, res.PlayerInstances...) {
		if f.owners[inst] != tableAccount {
			t.Errorf("force %s belongs to the table's account: %q", inst, f.owners[inst])
		}
	}
	for i, want := range [][]string{{"BMMadCat_H", "BMKoshi_E"}, {"BMGrizzly"}} {
		inst := res.PlayerInstances[i]
		names, crews := unitsOf(f.forces[inst])
		if !slices.Equal(names, want) {
			t.Errorf("player %d takes %v, has %v", i+1, want, names)
		}
		if got := f.forces[inst]["name"]; got != req.Players[i].Name || f.forces[inst]["instanceId"] != inst {
			t.Errorf("player %d's force is named and numbered for them: %v %v", i+1, got, f.forces[inst]["instanceId"])
		}
		if i == 0 && crews[0] != "Katarina" {
			t.Errorf("the crew the player assigned is applied: %v", crews)
		}
	}
	if names, _ := unitsOf(f.forces[hangar]); len(names) != 3 {
		t.Errorf("the hangar itself is untouched: %v", names)
	}
	// The unit IDs inside a player's force are the hangar's own, so damage and crews recorded there can be matched back to the hangar.
	if got := f.forces[res.PlayerInstances[0]]["groups"].([]any)[0].(map[string]any)["units"].([]any)[0].(map[string]any)["id"]; got != ids[0] {
		t.Errorf("a unit keeps its ID in the player's force: %v vs %v", got, ids[0])
	}
	// The operation sets every player's force against the OpFor's, with the forces' own timestamps.
	op := f.ops[res.OperationID]
	if op == nil || op["name"] != "Refusal War - Scenario 001" {
		t.Fatalf("the operation: %+v", op)
	}
	if want := []string{"friendly:" + res.PlayerInstances[0], "friendly:" + res.PlayerInstances[1], "enemy:" + res.OpForInstance}; !slices.Equal(operationForces(op), want) {
		t.Errorf("the operation's forces: %v, want %v", operationForces(op), want)
	}
	for _, fr := range op["forces"].([]any) {
		fm := fr.(map[string]any)
		if fm["timestamp"] != f.forces[fm["instanceId"].(string)]["timestamp"] {
			t.Errorf("an operation records each force's own timestamp: %v vs %v", fm["timestamp"], f.forces[fm["instanceId"].(string)]["timestamp"])
		}
	}
}

// A battle's status is its players' forces as MekBay has them now; the OpFor's is not read.
func TestReadBattleReadsThePlayersForces(t *testing.T) {
	f := newFakeForceServer(t)
	cl := &Client{URL: f.wsURL()}
	player := func(n int, unit string) BattlePlayer {
		return BattlePlayer{Number: n, Name: "P", Draft: ForceDraft{Name: "Player " + string(rune('0'+n)), Groups: []GroupDraft{{Name: "Deployed", Units: []UnitDraft{{Unit: unit, Pilot: "Alrek", Gunnery: 3, Piloting: 4}}}}}}
	}
	res, err := cl.StartBattle(context.Background(), BattleRequest{Account: tableAccount, Provided: true, OperationName: "S",
		OpFor:   BattleOpFor{Draft: ForceDraft{Name: "OpFor", Groups: []GroupDraft{{Name: "OpFor 1", Units: []UnitDraft{{Unit: "BMLoki_Prime"}}}}}},
		Players: []BattlePlayer{player(1, "BMBlackHawk_Prime"), player(2, "BMGrizzly")}})
	if err != nil {
		t.Fatal(err)
	}
	// Damage recorded on a player's force in MekBay shows in the status.
	f.mu.Lock()
	unit := f.forces[res.PlayerInstances[0]]["groups"].([]any)[0].(map[string]any)["units"].([]any)[0].(map[string]any)
	unit["state"].(map[string]any)["locations"] = map[string]any{"CT": map[string]any{"armor": float64(7)}}
	f.mu.Unlock()
	got, err := cl.ReadBattle(context.Background(), res.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Forces) != 2 || got.Forces[0].Alignment != "friendly" || got.Forces[1].Alignment != "friendly" || got.Forces[0].Force.Name != "Player 1" || got.Forces[1].Force.Name != "Player 2" {
		t.Fatalf("only the players' forces, in order: %+v", got.Forces)
	}
	if u := got.Forces[0].Force.Units[0]; u.Unit != "BMBlackHawk_Prime" || u.Pilot != "Alrek" || u.Locations["CT"].Armor != 7 {
		t.Errorf("a player's mech as MekBay has it now, damage and all: %+v", u)
	}
	if _, err := cl.ReadBattle(context.Background(), NewID()); err == nil || !strings.Contains(err.Error(), "no such operation") {
		t.Errorf("an operation that isn't there: %v", err)
	}
}

func TestStartBattleKeepsTheOperationAndOpFor(t *testing.T) {
	f := newFakeForceServer(t)
	cl := &Client{URL: f.wsURL()}
	draft := ForceDraft{Name: "OpFor", Groups: []GroupDraft{{Name: "OpFor 1", Units: []UnitDraft{{Unit: "BMLoki_Prime"}}}}}
	battle := func(op BattleOpFor, operation string) BattleRequest {
		return BattleRequest{Account: tableAccount, Provided: true, OperationName: "Scenario", OperationID: operation, OpFor: op, Players: []BattlePlayer{
			{Number: 1, Name: "P1", Draft: ForceDraft{Name: "P1", Groups: []GroupDraft{{Name: "Deployed", Units: []UnitDraft{{Unit: "BMBlackHawk_Prime", Pilot: "Alrek", Gunnery: 3, Piloting: 4}}}}}},
		}}
	}
	// A battle whose mechs are provided has no hangar: each force is built from its draft.
	first, err := cl.StartBattle(context.Background(), battle(BattleOpFor{Draft: draft}, ""))
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	names, crews := unitsOf(f.forces[first.PlayerInstances[0]])
	f.forces[first.OpForInstance]["marker"] = "damage"
	f.mu.Unlock()
	if !slices.Equal(names, []string{"BMBlackHawk_Prime"}) || crews[0] != "Alrek" {
		t.Errorf("a provided mech is built from the draft: %v %v", names, crews)
	}
	// Starting again with the OpFor and the operation kept updates the same operation, reuses the OpFor, and gives the players fresh forces.
	again, err := cl.StartBattle(context.Background(), battle(BattleOpFor{Draft: draft, Reuse: first.OpForInstance}, first.OperationID))
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	marker := f.forces[first.OpForInstance]["marker"]
	forces := operationForces(f.ops[first.OperationID])
	nOps := len(f.ops)
	f.mu.Unlock()
	if again.OperationID != first.OperationID || again.OpForInstance != first.OpForInstance || marker != "damage" || again.PlayerInstances[0] == first.PlayerInstances[0] {
		t.Errorf("the operation and the OpFor are kept and the players get fresh forces: %+v then %+v (%v)", first, again, marker)
	}
	if want := []string{"friendly:" + again.PlayerInstances[0], "enemy:" + first.OpForInstance}; !slices.Equal(forces, want) || nOps != 1 {
		t.Errorf("the one operation now holds the new forces: %v (%d operations)", forces, nOps)
	}
	// An OpFor that has vanished from MekBay says so, instead of starting a battle against nothing.
	if _, err := cl.StartBattle(context.Background(), battle(BattleOpFor{Draft: draft, Reuse: NewID()}, "")); err == nil || !strings.Contains(err.Error(), "reset the OpFor") {
		t.Errorf("a missing OpFor: %v", err)
	}
	if _, err := cl.StartBattle(context.Background(), BattleRequest{Account: tableAccount, OpFor: BattleOpFor{Draft: draft}}); err == nil {
		t.Error("a battle needs players")
	}
	if _, err := cl.StartBattle(context.Background(), BattleRequest{Account: tableAccount, Hangar: []byte("not a force"), OpFor: BattleOpFor{Draft: draft}, Players: []BattlePlayer{{Number: 1, Units: []string{"x"}}}}); err == nil || !strings.Contains(err.Error(), "hangar") {
		t.Errorf("a hangar that is not a force: %v", err)
	}
	// Forces owned by another account can't be touched.
	f.mu.Lock()
	f.owners[first.OpForInstance] = "someone-else"
	f.mu.Unlock()
	if _, err := cl.StartBattle(context.Background(), battle(BattleOpFor{Draft: draft, Reuse: first.OpForInstance}, first.OperationID)); err == nil || !strings.Contains(err.Error(), "gone") {
		t.Errorf("an OpFor owned by someone else is not ours to reuse: %v", err)
	}
}

func TestSubsetForce(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	hangar := map[string]any{
		"instanceId": "h", "name": "Hangar", "timestamp": "old", "owned": true, "version": float64(1), "type": "cbt", "bv": float64(99),
		"groups": []any{
			map[string]any{"id": "g1", "name": "Hangar", "units": []any{
				map[string]any{"id": "u1", "unit": "A", "state": map[string]any{"locations": map[string]any{"CT": map[string]any{"armor": float64(5)}}}},
				map[string]any{"id": "u2", "unit": "B"},
			}},
			map[string]any{"id": "g2", "name": "Deployed", "units": []any{map[string]any{"id": "u3", "unit": "C"}}},
		},
	}
	got, err := subsetForce(hangar, []string{"u3", "u1"}, "new-id", "Mine", now)
	if err != nil {
		t.Fatal(err)
	}
	if got["instanceId"] != "new-id" || got["name"] != "Mine" || got["timestamp"] == "old" || got["version"] != float64(1) || got["type"] != "cbt" {
		t.Errorf("the force gets its own identity and keeps the rest: %v", got)
	}
	if _, owned := got["owned"]; owned {
		t.Error("the server's own `owned` flag is never sent back")
	}
	gs := got["groups"].([]any)
	if len(gs) != 1 || gs[0].(map[string]any)["name"] != "Deployed" {
		t.Fatalf("one group: %v", gs)
	}
	us := gs[0].(map[string]any)["units"].([]any)
	if len(us) != 2 || us[0].(map[string]any)["id"] != "u1" || us[1].(map[string]any)["id"] != "u3" {
		t.Errorf("the units taken, in hangar order: %v", us)
	}
	// Damage travels with the unit, and the copy is independent of the hangar.
	us[0].(map[string]any)["state"].(map[string]any)["locations"].(map[string]any)["CT"].(map[string]any)["armor"] = float64(0)
	if hangar["groups"].([]any)[0].(map[string]any)["units"].([]any)[0].(map[string]any)["state"].(map[string]any)["locations"].(map[string]any)["CT"].(map[string]any)["armor"] != float64(5) {
		t.Error("changing the copy must not change the hangar")
	}
	if _, err := subsetForce(hangar, []string{"u1", "nope", "alsonope"}, "x", "y", now); err == nil || !strings.Contains(err.Error(), "alsonope, nope") {
		t.Errorf("units that are not there: %v", err)
	}
}

// A scenario's allies are one more force on the players' side, after theirs and before the OpFor, owned by the same account; ReadBattle reads them back with
// their instance ID, and the write-back leaves a skipped force alone.
func TestStartBattleWithAllies(t *testing.T) {
	f := newFakeForceServer(t)
	cl := &Client{URL: f.wsURL()}
	res, err := cl.StartBattle(context.Background(), BattleRequest{Account: tableAccount, Provided: true, OperationName: "S",
		OpFor:   BattleOpFor{Draft: ForceDraft{Name: "OpFor", Groups: []GroupDraft{{Name: "OpFor 1", Units: []UnitDraft{{Unit: "BMMadCat_S"}}}}}},
		Allies:  ForceDraft{Name: "S - Allies", Groups: []GroupDraft{{Name: "Allies", Units: []UnitDraft{{Unit: "BMKoshi_C", Pilot: "Vesna", Gunnery: 3, Piloting: 2}, {Unit: "SVSaviorRepairVehicle"}}}}},
		Players: []BattlePlayer{{Number: 1, Name: "P1", Draft: ForceDraft{Name: "P1", Groups: []GroupDraft{{Name: "Deployed", Units: []UnitDraft{{Unit: "BMGrizzly"}}}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	op := f.ops[res.OperationID]
	want := []string{"friendly:" + res.PlayerInstances[0], "friendly:" + res.AlliesInstance, "enemy:" + res.OpForInstance}
	if res.AlliesInstance == "" || !slices.Equal(operationForces(op), want) || f.owners[res.AlliesInstance] != tableAccount {
		t.Errorf("the allies follow the players on the friendly side: %v, want %v", operationForces(op), want)
	}
	if names, crews := unitsOf(f.forces[res.AlliesInstance]); !slices.Equal(names, []string{"BMKoshi_C", "SVSaviorRepairVehicle"}) || crews[0] != "Vesna" {
		t.Errorf("the allies' units: %v %v", names, crews)
	}
	f.mu.Unlock()
	got, err := cl.ReadBattle(context.Background(), res.OperationID)
	if err != nil || len(got.Forces) != 2 || got.Forces[1].InstanceID != res.AlliesInstance {
		t.Fatalf("the status reads the allies too, with their ID: %+v %v", got.Forces, err)
	}
	// None with a scenario that has no allies.
	plain, err := cl.StartBattle(context.Background(), BattleRequest{Account: tableAccount, Provided: true, OperationName: "S",
		OpFor:   BattleOpFor{Draft: ForceDraft{Name: "OpFor", Groups: []GroupDraft{{Name: "OpFor 1", Units: []UnitDraft{{Unit: "BMMadCat_S"}}}}}},
		Players: []BattlePlayer{{Number: 1, Name: "P1", Draft: ForceDraft{Name: "P1", Groups: []GroupDraft{{Name: "Deployed", Units: []UnitDraft{{Unit: "BMGrizzly"}}}}}}}})
	if err != nil || plain.AlliesInstance != "" {
		t.Errorf("no allies, no allies' force: %+v %v", plain, err)
	}
}

// A start that fails leaves nothing behind in MekBay: a plan that can't be built saves nothing at all, and a save that fails part-way deletes the forces saved
// before it (the caller gets no result, so nothing else would ever delete them). A reused OpFor is not this start's to delete.
func TestStartBattleLeavesNothingBehindWhenItFails(t *testing.T) {
	f := newFakeForceServer(t)
	cl := &Client{URL: f.wsURL()}
	f.mu.Lock()
	hangar, ids := f.saveHangar(tableAccount, ForceDraft{Name: "Hangar", Groups: []GroupDraft{{Name: "G", Units: []UnitDraft{{Unit: "BMMadCat_H"}, {Unit: "BMKoshi_E"}}}}})
	f.mu.Unlock()
	opfor := BattleOpFor{Draft: ForceDraft{Name: "OpFor", Groups: []GroupDraft{{Name: "OpFor 1", Units: []UnitDraft{{Unit: "BMLoki_Prime"}}}}}}
	allies := ForceDraft{Name: "Allies", Groups: []GroupDraft{{Name: "Allies", Units: []UnitDraft{{Unit: "BMKoshi_C"}}}}}
	left := func() (forces []string, ops, saves int) {
		f.mu.Lock()
		defer f.mu.Unlock()
		for inst := range f.forces {
			if inst != hangar {
				forces = append(forces, inst)
			}
		}
		return forces, len(f.ops), f.saves
	}

	// Player 2 takes a unit that is not in the hangar, and then a crew for a mech they didn't take: neither start saves the OpFor or player 1's force first.
	for name, p2 := range map[string]BattlePlayer{
		"a unit not in the hangar":    {Number: 2, Name: "P2", Units: []string{"no-such-unit"}},
		"a crew for a mech not taken": {Number: 2, Name: "P2", Units: []string{ids[1]}, Crew: []CrewAssignment{{UnitID: ids[0], Name: "Alrek", Gunnery: 3, Piloting: 4}}},
	} {
		_, err := cl.StartBattle(context.Background(), BattleRequest{Account: tableAccount, Hangar: f.hangarJSON(hangar), OperationName: "S", OpFor: opfor,
			Players: []BattlePlayer{{Number: 1, Name: "P1", Units: []string{ids[0]}}, p2}})
		if err == nil {
			t.Fatalf("%s: the start should fail", name)
		}
		if forces, ops, saves := left(); len(forces) != 0 || ops != 0 || saves != 0 {
			t.Errorf("%s: nothing is saved before every force is built: %v, %d operations, %d saves", name, forces, ops, saves)
		}
	}

	// The allies' force can't be saved, after the OpFor's and the player's were: both are deleted again.
	f.mu.Lock()
	f.refuse = "Allies"
	f.mu.Unlock()
	good := BattleRequest{Account: tableAccount, Hangar: f.hangarJSON(hangar), OperationName: "S", OpFor: opfor, Allies: allies,
		Players: []BattlePlayer{{Number: 1, Name: "P1", Units: []string{ids[0]}}}}
	if _, err := cl.StartBattle(context.Background(), good); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("a refused save fails the start: %v", err)
	}
	if forces, ops, saves := left(); len(forces) != 0 || ops != 0 || saves != 2 {
		t.Errorf("the forces saved before the failure are deleted: %v left, %d operations, %d saved", forces, ops, saves)
	}

	// With an OpFor kept from an earlier start, the failure deletes the new forces and leaves that OpFor alone.
	f.mu.Lock()
	f.refuse = ""
	f.mu.Unlock()
	first, err := cl.StartBattle(context.Background(), good)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.refuse = "Allies"
	f.mu.Unlock()
	again := good
	again.OpFor.Reuse, again.OperationID = first.OpForInstance, first.OperationID
	if _, err := cl.StartBattle(context.Background(), again); err == nil {
		t.Fatal("the second start should fail")
	}
	forces, ops, _ := left()
	slices.Sort(forces)
	want := []string{first.OpForInstance, first.PlayerInstances[0], first.AlliesInstance}
	slices.Sort(want)
	if !slices.Equal(forces, want) || ops != 1 {
		t.Errorf("the earlier start's forces and operation are all that is left: %v, want %v (%d operations)", forces, want, ops)
	}
}
