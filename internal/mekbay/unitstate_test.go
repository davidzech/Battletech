package mekbay

import (
	"encoding/json"
	"reflect"
	"testing"
)

// writeBack writes one battle unit (its state as JSON) back into a hangar holding the same unit undamaged, and returns the hangar's force and the unit as the site
// reads it.
func writeBack(t *testing.T, design, battleState string) (map[string]any, HangarUnit) {
	t.Helper()
	var st map[string]any
	if err := json.Unmarshal([]byte(battleState), &st); err != nil {
		t.Fatal(err)
	}
	hangar := hangarWith(unitMap("u", design, map[string]any{"crew": []any{map[string]any{"name": ""}}}))
	if rep := CommitState(hangar, map[string]map[string]any{"u": unitMap("u", design, st)}); rep.Matched != 1 {
		t.Fatalf("write-back: %+v", rep)
	}
	return hangar, readBack(t, hangar)
}

// A mission is often completed right after the last shots, before MekBay's phase ends: the damage still pending is written back as committed, the slots of a
// location whose structure is gone are destroyed with it, and three engine slots destroy the unit, as MekBay's evaluateDestroyed has it. A Timber Wolf (75 tons,
// center torso structure 23) whose center torso goes only in the pending damage is truly destroyed in the hangar.
func TestCommitStateSettlesPendingDamage(t *testing.T) {
	hangar, hu := writeBack(t, "BMMadCat_H", `{"destroyed":false,
		"locations":{"CT":{"armor":5,"pendingArmor":3,"internal":10,"pendingInternal":13},"LL":{"armor":2}},
		"crits":[{"name":"Engine","loc":"CT","slot":0,"destroying":100},{"name":"Engine","loc":"CT","slot":1},{"name":"Engine","loc":"CT","slot":2},{"name":"Gyro","loc":"CT","slot":3,"hits":0},
			{"name":"Hip","loc":"LL","slot":0,"hits":0,"pendingHits":1,"hitTimestamps":[5],"pendingHitTimestamps":[9]}],
		"crew":[{"name":"Alrek","gunnerySkill":2,"pilotingSkill":3,"hits":1,"state":0}]}`)
	st := stateOf(hangar, "u")
	ct := st["locations"].(map[string]any)["CT"].(map[string]any)
	if num(ct["armor"]) != 8 || num(ct["internal"]) != 23 || ct["pendingArmor"] != nil || ct["pendingInternal"] != nil {
		t.Errorf("the pending loss is committed: %+v", ct)
	}
	for _, c := range st["crits"].([]any) {
		cm := c.(map[string]any)
		if cm["loc"] == "CT" && (cm["destroyed"] == nil || cm["destroying"] == nil) {
			t.Errorf("a slot of the destroyed center torso is destroyed with it: %+v", cm)
		}
		if cm["name"] == "Hip" {
			if num(cm["hits"]) != 1 || cm["pendingHits"] != nil || cm["pendingHitTimestamps"] != nil || cm["destroyed"] == nil ||
				!reflect.DeepEqual(cm["hitTimestamps"], []float64{5, 9}) {
				t.Errorf("the pending hit is committed, its timestamps merged, and a hit slot is destroyed: %+v", cm)
			}
		}
	}
	if st["destroyed"] != true {
		t.Errorf("three engine slots destroy the unit (MekBay's evaluateDestroyed): %v", st["destroyed"])
	}
	if c, _ := ConditionOf(hu, 75, "XL (Clan)"); c != CondTruly || hu.EngineHits != 3 || !crewIsUnnamed(st) {
		t.Errorf("read back: %s, %d engine hits, crew %+v", c, hu.EngineHits, st["crew"])
	}
}

// A Clan XL engine has two slots in each side torso: a side torso lost in the pending damage takes them (being destroyed, or destroyed with it), which cripples
// the unit (two engine hits) without destroying it.
func TestCommitStateClanXLSideTorso(t *testing.T) {
	hangar, hu := writeBack(t, "BMMadCat_H", `{"destroyed":false,
		"locations":{"LT":{"armor":0,"pendingArmor":20,"internal":4,"pendingInternal":12}},
		"crits":[{"name":"Engine","loc":"LT","slot":0,"destroying":7},{"name":"Engine","loc":"LT","slot":1,"hits":0}],"crew":[]}`)
	if st := stateOf(hangar, "u"); st["destroyed"] == true {
		t.Error("two engine slots don't destroy the unit")
	}
	if c, why := ConditionOf(hu, 75, "XL (Clan)"); c != CondCrippled || hu.EngineHits != 2 || hu.IsDestroyed() {
		t.Errorf("crippled by two engine hits: %s (%s), %d hits", c, why, hu.EngineHits)
	}
}

// What only means something on the table is dropped: heat, the turn's state, the unit's battle conditions (prone, shutdown...) and NARC pods. A location
// condition still pending (a leg blown off this phase) is committed.
func TestCommitStateClearsBattleLeftovers(t *testing.T) {
	hangar, hu := writeBack(t, "BMKoshi_E", `{"destroyed":false,"heat":{"current":12,"previous":8,"next":3,"heatsinksOff":2},
		"turnState":{"turnCounter":4,"moveMode":"run"},"conditions":["prone",{"key":"shutdown"},"jammed"],
		"locations":{"RA":{"conditions":["narc",{"key":"narc","value":2}]},"LL":{"armor":3,"conditions":[{"key":"blown-off","pending":true},"narc"]}},
		"crits":[],"crew":[]}`)
	st := stateOf(hangar, "u")
	if h := st["heat"].(map[string]any); num(h["current"]) != 0 || num(h["previous"]) != 0 || len(h) != 2 {
		t.Errorf("heat is reset: %v", h)
	}
	if st["turnState"] != nil || st["conditions"] != nil {
		t.Errorf("the turn and the unit's conditions are dropped: %v %v", st["turnState"], st["conditions"])
	}
	locs := st["locations"].(map[string]any)
	if locs["RA"] != nil {
		t.Errorf("a location with only a NARC pod is left undamaged: %v", locs["RA"])
	}
	if ll := locs["LL"].(map[string]any); !reflect.DeepEqual(ll["conditions"], []any{"blown-off"}) {
		t.Errorf("the blown-off leg is committed and its NARC pod gone: %v", ll["conditions"])
	}
	if !hu.Locations["LL"].Gone || !InternalGone(hu, "LL", 25) {
		t.Errorf("the blown-off leg reads as destroyed: %+v", hu.Locations)
	}
}

// Equipment destroyed on the inventory (pending too) is written back as destroyed, and reads as damage.
func TestCommitStateCommitsInventory(t *testing.T) {
	hangar, hu := writeBack(t, "BMKoshi_E", `{"destroyed":false,"crits":[],"crew":[],
		"inventory":[{"id":"CLERMediumLaser@RA#0","destroying":true},{"id":"CLERMediumLaser@LA#1","destroyed":true,"destroying":false},{"id":"Booby Trap@CT#2","consumed":1}]}`)
	inv := stateOf(hangar, "u")["inventory"].([]any)
	if a, b := inv[0].(map[string]any), inv[1].(map[string]any); a["destroyed"] != true || a["destroying"] != nil || b["destroyed"] != nil || b["destroying"] != nil {
		t.Errorf("pending destruction committed, a pending repair too: %v %v", a, b)
	}
	if !hu.Damaged || len(hu.Crits) != 1 || hu.Crits[0].Loc != "RA" || hu.AmmoBins != 1 {
		t.Errorf("one destroyed item and one used up: %+v", hu)
	}
}
