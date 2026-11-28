package mekbay

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/davidzech/Battletech/internal/catalog"
	"testing"
	"time"
)

// A version 1 hangar with one damaged Timber Wolf A and one pristine Loki Prime. The Timber Wolf has lost front and rear center torso armor and left torso
// structure, its left arm is blown off, its engine took a hit (CT slot 0), its Streak SRM 6 bin (RT slot 4) fired 5 shots, and its pilot has two hits.
const v1Hangar = `{"version":1,"timestamp":"2026-11-20T10:00:00.000Z","instanceId":"hangar-1","type":"cbt","name":"Wolves","groups":[
 {"id":"g1","name":"Hangar","color":"","units":[
  {"id":"0195a0b1-0000-7000-8000-000000000001","unit":"BMMadCat_A","commander":true,"state":{
   "crew":[{"id":0,"name":"Aidan","gunnerySkill":3,"pilotingSkill":4,"hits":2,"state":0}],
   "locations":{"CT":{"armor":6},"CT-rear":{"armor":2},"LT":{"internal":4},"LA":{"conditions":["blown-off"]}},
   "crits":[{"id":"x","name":"Fusion Engine","loc":"CT","slot":0,"hits":1,"destroyed":1700000000000},
            {"id":"y","name":"Clan Streak SRM 6 Ammo","loc":"RT","slot":4,"consumed":5}],
   "destroyed":false}},
  {"id":"0195a0b1-0000-7000-8000-000000000002","unit":"BMLoki_Prime","state":{"crew":[{"id":0,"name":"","gunnerySkill":4,"pilotingSkill":5}],"crits":[],"locations":{}}}]}]}`

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func view(t *testing.T, force map[string]any) HangarForce {
	t.Helper()
	b, _ := json.Marshal(force)
	h, err := ParseForce(b)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func unitByID(h HangarForce, id string) HangarUnit {
	for _, u := range h.Units {
		if u.ID == id {
			return u
		}
	}
	return HangarUnit{}
}

const wolf, loki = "0195a0b1-0000-7000-8000-000000000001", "0195a0b1-0000-7000-8000-000000000002"

func v2Hangar(t *testing.T) map[string]any {
	t.Helper()
	f, err := ConvertToV2(decode(t, v1Hangar))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// Converting keeps what the site sees of every unit: IDs, designs, groups, crews, damage, engine hits, spent ammunition.
func TestConvertToV2KeepsTheView(t *testing.T) {
	before := view(t, decode(t, v1Hangar))
	f := v2Hangar(t)
	if ForceVersion(f) != 2 || f["name"] != "Wolves" || f["instanceId"] != "hangar-1" {
		t.Fatalf("converted header: %v %v %v", f["version"], f["name"], f["instanceId"])
	}
	after := view(t, f)
	if len(after.Units) != 2 {
		t.Fatalf("%d units", len(after.Units))
	}
	b, a := unitByID(before, wolf), unitByID(after, wolf)
	if a.Unit != "BMMadCat_A" || a.Group != "Hangar" || a.Pilot != "Aidan" || a.Gunnery != 3 || a.Piloting != 4 {
		t.Errorf("identity and crew: %+v", a)
	}
	if a.Locations["CT"].Armor != b.Locations["CT"].Armor+b.Locations["CT-rear"].Armor || a.Locations["LT"].Internal != 4 || !a.Locations["LA"].Gone {
		t.Errorf("locations: v1 %+v, v2 %+v", b.Locations, a.Locations)
	}
	if a.EngineHits != 1 || b.EngineHits != 1 || len(a.Crits) != 1 {
		t.Errorf("engine hits v1 %d v2 %d, crits %+v", b.EngineHits, a.EngineHits, a.Crits)
	}
	if a.AmmoBins != 1 || a.AmmoUsed != 5 || a.AmmoUsed2[0].Name != "Clan Streak SRM 6 Ammo" || a.AmmoUsed2[0].Loc != "RT" {
		t.Errorf("ammo: %+v", a.AmmoUsed2)
	}
	if l := unitByID(after, loki); l.Unit != "BMLoki_Prime" || l.Damaged {
		t.Errorf("loki: %+v", l)
	}
	// The commander mark moves to the pilot; the slot hit is a destroyed slot; spent ammo goes on component m14.
	u := v2Units(f)[0]
	if p := v2Pilot(u); p["commander"] != true || p["health"].(map[string]any)["wounds"] != 2 {
		t.Errorf("pilot: %v", p)
	}
	st := u["state"].(map[string]any)
	if s := st[v2SlotState].([]any)[0].([]any); s[0] != "s:ct:0" || s[1] != 1 || s[2] != 1 {
		t.Errorf("slot row: %v", s)
	}
	if a := st[v2AmmoState].([]any)[0].([]any); a[0] != "m:m14" || a[1] != 5 {
		t.Errorf("ammo row: %v", a)
	}
}

func TestV2AddRemoveSwap(t *testing.T) {
	f := v2Hangar(t)
	if err := AddUnitsToForce(f, []string{"BMGrizzly"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	h := view(t, f)
	if len(h.Units) != 3 || h.Units[2].Unit != "BMGrizzly" || h.Units[2].Group != "Hangar" {
		t.Fatalf("after purchase: %+v", h.Units)
	}
	grizzly := h.Units[2].ID
	if err := AddUnitsToForce(f, []string{"NoSuchMech"}, time.Now()); err == nil {
		t.Error("bought a mech the catalog doesn't have")
	}
	if !RemoveUnitFromForce(f, wolf) || RemoveUnitFromForce(f, wolf) {
		t.Fatal("removing the Timber Wolf")
	}
	h = view(t, f)
	if len(h.Units) != 2 || h.Units[0].ID != loki || h.Units[1].ID != grizzly || h.Units[1].Group != "Hangar" {
		t.Fatalf("after sale: %+v", h.Units)
	}
	if err := SwapUnitState(f, loki, "BMLoki_A", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := SwapUnitState(f, loki, "BMLoki_A", time.Now()); !errors.Is(err, ErrNoChange) {
		t.Errorf("second swap: %v", err)
	}
	if u := unitByID(view(t, f), loki); u.Unit != "BMLoki_A" || u.Damaged {
		t.Errorf("swapped: %+v", u)
	}
}

func TestV2RepairAndRearm(t *testing.T) {
	// A unit that is not crippled: the structure level fixes everything (the blown-off arm and the engine hit too), and leaves the ammunition.
	f := v2Hangar(t)
	if err := ServiceUnitState(f, wolf, "repair", RepairArmor, 75, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	u := unitByID(view(t, f), wolf)
	if u.Locations["CT"].Armor != 0 || u.Locations["LT"].Internal != 4 || u.EngineHits != 1 {
		t.Errorf("armor repair: %+v, engine %d", u.Locations, u.EngineHits)
	}
	if err := ServiceUnitState(f, wolf, "repair", RepairArmor, 75, false, time.Now()); !errors.Is(err, ErrNoChange) {
		t.Errorf("second armor repair: %v", err)
	}
	if err := ServiceUnitState(f, wolf, "repair", RepairStructure, 75, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	u = unitByID(view(t, f), wolf)
	if u.Damaged || u.EngineHits != 0 || u.AmmoBins != 1 {
		t.Errorf("structure repair: %+v", u)
	}
	if err := ServiceUnitState(f, wolf, "rearm", "", 75, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if u = unitByID(view(t, f), wolf); u.AmmoBins != 0 {
		t.Errorf("rearm: %+v", u.AmmoUsed2)
	}
	if err := ServiceUnitState(f, wolf, "rearm", "", 75, false, time.Now()); !errors.Is(err, ErrNoChange) {
		t.Errorf("second rearm: %v", err)
	}

	// Crippled: the structure level leaves the blown-off arm and the engine hit to the crippled level.
	f = v2Hangar(t)
	if err := ServiceUnitState(f, wolf, "repair", RepairStructure, 75, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	u = unitByID(view(t, f), wolf)
	if !u.Locations["LA"].Gone || u.EngineHits != 1 || u.Locations["LT"].Internal != 0 || u.Locations["CT"].Armor != 0 {
		t.Errorf("crippled structure repair: %+v, engine %d", u.Locations, u.EngineHits)
	}
	if err := ServiceUnitState(f, wolf, "repair", RepairCrippled, 75, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if u = unitByID(view(t, f), wolf); u.Damaged {
		t.Errorf("crippled repair: %+v", u)
	}
}

// A unit destroyed by its cockpit gets MekBay's flag from a lower repair, so only the destroyed level makes it whole.
func TestV2DestroyedNeedsTheDestroyedRepair(t *testing.T) {
	f := v2Hangar(t)
	_, um := v2Find(f, loki)
	um["state"] = map[string]any{v2SlotState: []any{[]any{"s:hd:2", 1, 1}}}
	if u := unitByID(view(t, f), loki); !u.CockpitGone || !u.IsDestroyed() {
		t.Fatalf("cockpit hit: %+v", u)
	}
	if err := ServiceUnitState(f, loki, "repair", RepairCrippled, 65, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if u := unitByID(view(t, f), loki); !u.Destroyed || u.Damaged {
		t.Errorf("after the crippled repair: %+v", u)
	}
	if err := ServiceUnitState(f, loki, "repair", RepairDestroyed, 65, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if u := unitByID(view(t, f), loki); u.IsDestroyed() {
		t.Errorf("after the destroyed repair: %+v", u)
	}
}

func TestV2SubsetAndCrew(t *testing.T) {
	h := v2Hangar(t)
	f, err := v2Subset(h, []string{loki}, "battle-1", "Player 1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v2Subset(h, []string{"nope"}, "x", "x", time.Now()); err == nil {
		t.Error("took a unit the hangar doesn't have")
	}
	if err := applyCrew(f, []CrewAssignment{{UnitID: loki, Name: "Natasha", Gunnery: 2, Piloting: 3, Edge: 2}}); err != nil {
		t.Fatal(err)
	}
	setCommander(f, loki)
	v := view(t, f)
	if len(v.Units) != 1 || v.Units[0].Pilot != "Natasha" || v.Units[0].Gunnery != 2 || v.Name != "Player 1" {
		t.Errorf("battle force: %+v", v)
	}
	if p := v2Pilot(v2Units(f)[0]); p["commander"] != true || p["edge"] != 2 {
		t.Errorf("pilot: %v", p)
	}
	// Taking the pilot off the unit takes their Edge with them; Edge past MekBay's cap is capped.
	if err := applyCrew(f, []CrewAssignment{{UnitID: loki}}); err != nil {
		t.Fatal(err)
	}
	if p := v2Pilot(v2Units(f)[0]); p["edge"] != nil || p["name"] != nil {
		t.Errorf("cleared pilot: %v", p)
	}
	if err := applyCrew(f, []CrewAssignment{{UnitID: loki, Name: "Natasha", Gunnery: 2, Piloting: 3, Edge: 150}}); err != nil {
		t.Fatal(err)
	}
	if p := v2Pilot(v2Units(f)[0]); p["edge"] != maxEdge {
		t.Errorf("capped edge: %v", p["edge"])
	}
	v2Pristine(v2Units(f)[0])
	if p := v2Pilot(v2Units(f)[0]); p["edge"] != nil {
		t.Errorf("pristine pilot kept Edge: %v", p)
	}
	if err := applyCrew(f, []CrewAssignment{{UnitID: "nope", Name: "X"}}); err == nil {
		t.Error("crewed a unit the force doesn't have")
	}
	// The hangar itself never holds the named pilot.
	if u := unitByID(view(t, h), loki); u.Pilot != "" {
		t.Errorf("hangar pilot: %q", u.Pilot)
	}
}

func TestBuildForceV2(t *testing.T) {
	f, err := BuildForceV2("opfor-1", time.Now(), ForceDraft{Name: "OpFor", Groups: []GroupDraft{{Name: "Star", Units: []UnitDraft{
		{Unit: "BMGrizzly", BV: 1500, Pilot: "Drusus", Gunnery: 3, Piloting: 4, Edge: 3, Commander: true}, {Unit: "BMLoki_Prime", BV: 2000, Gunnery: 4, Piloting: 5, Edge: 1}}}}})
	if err != nil {
		t.Fatal(err)
	}
	v := view(t, f)
	if len(v.Units) != 2 || v.Units[0].Pilot != "Drusus" || v.Units[0].Group != "Star" || v.Units[1].Gunnery != 4 || f["bv"] != 3500 {
		t.Errorf("built: %+v", v)
	}
	// A named pilot brings their Edge; an unnamed crew has none.
	if us := v2Units(f); v2Pilot(us[0])["edge"] != 3 || v2Pilot(us[1])["edge"] != nil {
		t.Errorf("edge: %v, %v", v2Pilot(us[0]), v2Pilot(us[1]))
	}
	if _, err := BuildForceV2("x", time.Now(), ForceDraft{Groups: []GroupDraft{{Units: []UnitDraft{{Unit: "NoSuchMech"}}}}}); err == nil {
		t.Error("built a mech the catalog doesn't have")
	}
}

// A battle that comes back in version 2 makes a version 1 hangar version 2, its rows settled: pending damage added in, the pilot reset, battle leftovers gone.
func TestCommitV2BattleIntoV1Hangar(t *testing.T) {
	h := decode(t, v1Hangar)
	battle := map[string]map[string]any{
		loki: {v2Marker: true, "id": packInstanceID(loki), "uuid": mustPack(t, "BMLoki_Prime"), "crew": []any{map[string]any{"id": "~p", "name": "Natasha", "g": 2}},
			"state": map[string]any{
				v2LocationState: []any{[]any{"f:ct", 5}},
				v2Pending:       map[string]any{v2LocationState: []any{[]any{"f:ct", 3}, []any{"i:rt", 2}}, v2SlotState: []any{[]any{"s:rt:3", 1}}},
				"z":             map[string]any{"c": 9}, "t": map[string]any{"n": 4}, "m": map[string]any{"a": "run"},
			}},
		"gone": {v2Marker: true, "id": "~x", "uuid": mustPack(t, "BMGrizzly")},
	}
	rep := CommitState(h, battle)
	if ForceVersion(h) != 2 || rep.Matched != 1 || len(rep.Skipped) != 1 {
		t.Fatalf("version %d, report %+v", ForceVersion(h), rep)
	}
	u := unitByID(view(t, h), loki)
	if u.Locations["CT"].Armor != 8 || u.Locations["RT"].Internal != 2 || len(u.Crits) != 1 || u.Pilot != "" || u.Gunnery != 4 {
		t.Errorf("committed: %+v", u)
	}
	_, um := v2Find(h, loki)
	st := um["state"].(map[string]any)
	for _, k := range []string{v2Pending, "z", "t", "m"} {
		if _, ok := st[k]; ok {
			t.Errorf("state keeps %s", k)
		}
	}
	if s := st[v2SlotState].([]any)[0].([]any); len(s) != 3 || s[2] != 1 {
		t.Errorf("a slot hit is a destroyed slot: %v", s)
	}
	// The other units came over unchanged.
	if w := unitByID(view(t, h), wolf); w.EngineHits != 1 || w.Pilot != "Aidan" {
		t.Errorf("timber wolf: %+v", w)
	}
}

// A version 1 battle unit committed into a version 2 hangar is converted on the way.
func TestCommitV1BattleIntoV2Hangar(t *testing.T) {
	h := v2Hangar(t)
	battle := map[string]map[string]any{loki: {"id": loki, "unit": "BMLoki_Prime", "state": map[string]any{
		"crew":      []any{map[string]any{"name": "Natasha", "gunnerySkill": 2, "pilotingSkill": 3}},
		"locations": map[string]any{"RA": map[string]any{"armor": 4, "pendingArmor": 1}},
	}}}
	if rep := CommitState(h, battle); rep.Matched != 1 {
		t.Fatalf("report %+v", rep)
	}
	if u := unitByID(view(t, h), loki); u.Locations["RA"].Armor != 5 || u.Pilot != "" {
		t.Errorf("committed: %+v", u)
	}
}

func mustPack(t *testing.T, unit string) string {
	t.Helper()
	d, err := designUUID(unit)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := packUUID(d)
	return p
}

// In version 2 mode, a hangar still in version 1 is converted when it is next edited; otherwise it stays as it is.
func TestUpgradeHangarFollowsTheMode(t *testing.T) {
	t.Cleanup(func() { SetV2(false) })
	f := decode(t, v1Hangar)
	if err := UpgradeHangar(f); err != nil || ForceVersion(f) != 1 {
		t.Fatalf("version 1 mode: %v, version %d", err, ForceVersion(f))
	}
	SetV2(true)
	if err := UpgradeHangar(f); err != nil || ForceVersion(f) != 2 {
		t.Fatalf("version 2 mode: %v, version %d", err, ForceVersion(f))
	}
	if len(view(t, f).Units) != 2 {
		t.Error("units lost")
	}
	b, err := NewHangar(ForceDraft{Name: "New", Groups: []GroupDraft{{Name: HangarGroup, Units: []UnitDraft{{Unit: "BMGrizzly"}}}}}, time.Now())
	if err != nil || ForceVersion(decode(t, string(b))) != 2 {
		t.Errorf("a new hangar in version 2 mode: %v %s", err, b)
	}
}

func TestCleanV2Force(t *testing.T) {
	f := v2Hangar(t)
	f["owned"] = true
	clean, err := CleanForce(f, "imported", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	v := view(t, clean)
	if clean["instanceId"] != "imported" || clean["owned"] != nil || len(v.Units) != 2 {
		t.Fatalf("cleaned: %v", clean)
	}
	for _, u := range v.Units {
		if u.Damaged || u.AmmoBins != 0 || u.Pilot != "" || u.Destroyed {
			t.Errorf("not pristine: %+v", u)
		}
	}
}

// In version 2 mode a battle is built in version 2 from a version 1 hangar: every force saved as MekBay's version 2 writer saves one, the players' units
// keeping their IDs and damage, and the battle read back and written into the hangar makes it version 2.
func TestStartAndCommitBattleV2(t *testing.T) {
	SetV2(true)
	t.Cleanup(func() { SetV2(false) })
	srv := newFakeForceServer(t)
	c := &Client{URL: srv.wsURL()}
	res, err := c.StartBattle(context.Background(), BattleRequest{
		Account: tableAccount, Hangar: []byte(v1Hangar), OperationName: "Trial",
		Players: []BattlePlayer{{Number: 1, Name: "Player 1", Units: []string{wolf}, Crew: []CrewAssignment{{UnitID: wolf, Name: "Aidan", Gunnery: 3, Piloting: 4}}, Commander: wolf}},
		OpFor:   BattleOpFor{Draft: ForceDraft{Name: "OpFor", Groups: []GroupDraft{{Name: "Star", Units: []UnitDraft{{Unit: "BMGrizzly", Gunnery: 3, Piloting: 4}}}}}},
		Allies:  ForceDraft{Name: "Allies", Groups: []GroupDraft{{Name: "Point", Units: []UnitDraft{{Unit: "BMLoki_Prime", Gunnery: 4, Piloting: 5}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, inst := range []string{res.OpForInstance, res.PlayerInstances[0], res.AlliesInstance} {
		m := srv.saveMsgs[inst]
		if ForceVersion(srv.forces[inst]) != 2 || m["forcePersistenceRevision"] != float64(2) || m["cbtPersistence"].(map[string]any)["writerVersion"] != float64(2) {
			t.Errorf("force %s saved as %v", inst, m)
		}
	}
	player := view(t, srv.forces[res.PlayerInstances[0]])
	if len(player.Units) != 1 || player.Units[0].ID != wolf || player.Units[0].Pilot != "Aidan" || player.Units[0].EngineHits != 1 {
		t.Fatalf("player force: %+v", player.Units)
	}
	if op := srv.ops[res.OperationID]; len(operationForces(op)) != 3 {
		t.Errorf("operation: %v", operationForces(op))
	}
	// The player's MekBay marks more damage; the battle is completed.
	um := v2Units(srv.forces[res.PlayerInstances[0]])[0]
	v2State(um)[v2Pending] = map[string]any{v2LocationState: []any{[]any{"i:rt", 3}}}
	battle, err := c.ReadBattleUnits(context.Background(), tableAccount, res.OperationID, []string{res.AlliesInstance})
	if err != nil {
		t.Fatal(err)
	}
	h := decode(t, v1Hangar)
	if rep := CommitState(h, battle); rep.Matched != 1 || len(rep.Skipped) != 0 {
		t.Fatalf("commit: %+v", rep)
	}
	w := unitByID(view(t, h), wolf)
	if ForceVersion(h) != 2 || w.Locations["RT"].Internal != 3 || w.EngineHits != 1 || w.Pilot != "" {
		t.Errorf("hangar after the battle: version %d, %+v", ForceVersion(h), w)
	}
}

// Each kind of unit gets the entity type MekBay reads from its design file; a Mek and an aerospace fighter get none.
func TestV2EntityType(t *testing.T) {
	for name, want := range map[string]string{
		"BMMadCat_Prime": "", "CVAC2Carrier": "Tank", "CVArmoredPersonnelCarrier_Hover": "Tank", "CVFensalirCombatWiGE": "Tank",
		"SVSaviorRepairVehicle": "SupportTank", "SVBoreasCavalryHovercraft_A": "SupportTank", "SVAdelantePassengerCargoTrain": "LargeSupportTank",
		"CVAeronStrikeVTOL": "VTOL", "SVAppleChurchillSurveillanceVTOL": "SupportVTOL", "SVBishopTransportVTOL": "SupportVTOL",
		"CVMaunaKeaCommandVessel": "Naval", "CVKoryuSubmarine_Civilian": "Naval", "SVAtlantiaLuxuryYacht": "SupportNaval",
		"BAElementalBattleArmor_APGaussSqd5": "BattleArmor", "CIAAJumpInfantry_292ndLeagueRegulars": "Infantry", "PMBasilisk": "ProtoMek",
	} {
		cu, ok := catalog.ByName(name)
		if !ok {
			t.Fatalf("%s is not in the catalog", name)
		}
		if got := v2EntityType(cu); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
}

// A non-Mek's row always carries its family and entity type, or MekBay reads it as a Mek and resets it: built, made pristine, and swapped.
func TestV2NonMekRowsCarryTheirFamily(t *testing.T) {
	family := func(um map[string]any) string {
		st, _ := um["state"].(map[string]any)
		k, _ := st[v2FamilyField].(string)
		ty, _ := st[v2EntityTypeField].(string)
		return k + "/" + ty
	}
	f, err := BuildForceV2("allies", time.Now(), ForceDraft{Name: "Allies", Groups: []GroupDraft{{Name: "Allies", Units: []UnitDraft{
		{Unit: "BMKoshi_C", Pilot: "Vesna", Gunnery: 3, Piloting: 2}, {Unit: "SVSaviorRepairVehicle", Pilot: "Savior crew", Gunnery: 4, Piloting: 5},
		{Unit: "BAElementalBattleArmor_APGaussSqd5", Gunnery: 4, Piloting: 5}}}}})
	if err != nil {
		t.Fatal(err)
	}
	us := v2Units(f)
	if us[0]["state"] != nil || family(us[1]) != "e/SupportTank" || family(us[2]) != "e/BattleArmor" {
		t.Errorf("built rows: %v / %v / %v", us[0]["state"], us[1]["state"], us[2]["state"])
	}
	v2Pristine(us[1])
	if family(us[1]) != "e/SupportTank" {
		t.Errorf("pristine: %v", us[1]["state"])
	}
	// Swapping a vehicle for a mech leaves no family behind; a mech for a vehicle gains one.
	if err := v2Swap(f, v2UnitID(us[1]), "BMKoshi_A"); err != nil {
		t.Fatal(err)
	}
	if us[1]["state"] != nil {
		t.Errorf("a mech kept a vehicle's family: %v", us[1]["state"])
	}
	if err := v2Swap(f, v2UnitID(us[0]), "CVAC2Carrier"); err != nil {
		t.Fatal(err)
	}
	if family(us[0]) != "e/Tank" {
		t.Errorf("swapped to a tank: %v", us[0]["state"])
	}
	// Settling a vehicle after a battle drops a Mek's turn ("t"), but not the vehicle's entity type.
	v2State(us[0])["l"] = []any{[]any{"i:fr", 1}}
	v2Settle(us[0])
	v2MarkFamily(us[0])
	if family(us[0]) != "e/Tank" {
		t.Errorf("settled: %v", us[0]["state"])
	}
}
