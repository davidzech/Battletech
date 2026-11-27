package mekbay

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestServiceUnitState(t *testing.T) {
	load := func() map[string]any {
		var f map[string]any
		if err := json.Unmarshal([]byte(crippledForce), &f); err != nil {
			t.Fatal(err)
		}
		return f
	}
	repaired := func(level string) HangarUnit {
		f := load()
		if err := ServiceUnitState(f, "c", "repair", level, 20, true, time.Now()); err != nil {
			t.Fatal(err)
		}
		return readBack(t, f)
	}
	before := readBack(t, load())
	if c, _ := ConditionOf(before, 20, "Fusion"); c != CondCrippled || before.AmmoBins != 1 || before.Locations["RA"].Armor != 5 {
		t.Fatalf("before: %s, %d bins", c, before.AmmoBins)
	}
	// Armor only: the armor goes, and the unit is just as crippled (its structure is untouched).
	a := repaired(RepairArmor)
	if c, _ := ConditionOf(a, 20, "Fusion"); c != CondCrippled || a.Locations["RA"].Armor != 0 || a.Locations["RA"].Internal != 3 || len(a.Crits) != 1 {
		t.Errorf("armor repair: %s %+v crits %d", c, a.Locations, len(a.Crits))
	}
	// Structure: nothing is left to fix in a surviving location, so the destroyed limbs, their crits and the cripple all stay.
	st := repaired(RepairStructure)
	if c, _ := ConditionOf(st, 20, "Fusion"); c != CondCrippled || st.Locations["LL"].Internal != 4 || len(st.Crits) != 1 || st.Locations["RA"].Armor != 0 {
		t.Errorf("structure repair: %s %+v crits %d", c, st.Locations, len(st.Crits))
	}
	// Crippled: everything is repaired, but spent ammunition waits for a rearm.
	cr := repaired(RepairCrippled)
	if c, _ := ConditionOf(cr, 20, "Fusion"); c != CondOK || cr.Destroyed || cr.AmmoBins != 1 || len(cr.Crits) != 0 {
		t.Errorf("crippled repair: %s destroyed=%v bins=%d crits=%d", c, cr.Destroyed, cr.AmmoBins, len(cr.Crits))
	}
	// A rearm refills the ammunition and leaves the damage.
	f := load()
	if err := ServiceUnitState(f, "c", "rearm", "", 20, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	ra := readBack(t, f)
	if c, _ := ConditionOf(ra, 20, "Fusion"); c != CondCrippled || ra.AmmoBins != 0 || ra.AmmoUsed != 0 {
		t.Errorf("after rearm: %s bins=%d used=%d", c, ra.AmmoBins, ra.AmmoUsed)
	}
	// The same service asked for twice (a double click, or two players at the table) changes nothing the second time, which is an error, so it isn't paid for twice.
	for _, again := range [][2]string{{"repair", RepairArmor}, {"repair", RepairStructure}, {"repair", RepairCrippled}, {"rearm", ""}} {
		f := load()
		if err := ServiceUnitState(f, "c", again[0], again[1], 20, true, time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := ServiceUnitState(f, "c", again[0], again[1], 20, true, time.Now()); !errors.Is(err, ErrNoChange) {
			t.Errorf("%s %s a second time: %v", again[0], again[1], err)
		}
	}
	// A lower repair after a higher one has nothing left to fix either; a rearm after a repair still has.
	f = load()
	if err := ServiceUnitState(f, "c", "repair", RepairCrippled, 20, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := ServiceUnitState(f, "c", "repair", RepairArmor, 20, true, time.Now()); !errors.Is(err, ErrNoChange) {
		t.Errorf("an armor repair after a full one: %v", err)
	}
	if err := ServiceUnitState(f, "c", "rearm", "", 20, true, time.Now()); err != nil {
		t.Errorf("a rearm after a full repair: %v", err)
	}
	if err := ServiceUnitState(load(), "nope", "repair", RepairArmor, 20, true, time.Now()); err == nil {
		t.Error("an unknown unit should be refused")
	}
	if err := ServiceUnitState(load(), "c", "paint", "", 20, true, time.Now()); err == nil {
		t.Error("an unknown service should be refused")
	}
	if err := ServiceUnitState(load(), "c", "repair", "polish", 20, true, time.Now()); err == nil {
		t.Error("an unknown repair level should be refused")
	}

	// A crippled unit with structure damage in a surviving location, a critical hit there and an engine hit: structure repair fixes the first two only.
	var g map[string]any
	_ = json.Unmarshal([]byte(`{"groups":[{"units":[{"id":"x","unit":"BMBaboon","state":{"destroyed":false,
	 "locations":{"CT":{"armor":3,"internal":2}},
	 "crits":[{"name":"Gyro","loc":"CT","destroyed":1,"hits":0},{"name":"Clan XL Engine","loc":"CT","destroyed":1,"hits":0}],"crew":[]}}]}]}`), &g)
	if err := ServiceUnitState(g, "x", "repair", RepairStructure, 20, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	x := readBack(t, g)
	if len(x.Locations) != 0 || len(x.Crits) != 1 || !x.Crits[0].Engine {
		t.Errorf("structure repair should leave only the engine hit: %+v %+v", x.Locations, x.Crits)
	}
}

// A unit that is neither crippled nor destroyed has no damage above the structure level: one destroyed limb, one engine hit, or a side torso lost on a
// fusion engine that passed its check are all restored by the structure repair, which leaves the unit undamaged.
// A unit whose cockpit is gone counts as destroyed even without MekBay's destroyed flag (damage written outside MekBay's app), and needs the destroyed repair
// like a flagged one: a crippled repair restores the head and the cockpit but leaves the unit destroyed (the flag is set), the destroyed repair brings it back.
func TestCockpitGoneIsDestroyedUntilRepaired(t *testing.T) {
	for name, state := range map[string]string{
		"head gone":    `{"destroyed":false,"locations":{"HD":{"armor":9,"internal":3}},"crits":[],"crew":[{"name":"","gunnerySkill":4,"pilotingSkill":5,"hits":0,"state":0}]}`,
		"cockpit crit": `{"destroyed":false,"locations":{"HD":{"armor":9,"internal":1}},"crits":[{"name":"Cockpit","loc":"HD","destroyed":1,"hits":0}],"crew":[]}`,
	} {
		for _, level := range []string{RepairDestroyed, RepairCrippled} {
			var f map[string]any
			if err := json.Unmarshal([]byte(`{"name":"t","groups":[{"name":"","units":[{"id":"c","unit":"BMBaboon","updatedTs":1,"state":`+state+`}]}]}`), &f); err != nil {
				t.Fatal(err)
			}
			before := readBack(t, f)
			if c, _ := ConditionOf(before, 20, "Fusion"); !before.IsDestroyed() || before.Destroyed || c != CondDestroyed {
				t.Fatalf("%s before: destroyed=%v flag=%v %s", name, before.IsDestroyed(), before.Destroyed, c)
			}
			if err := ServiceUnitState(f, "c", "repair", level, 20, true, time.Now()); err != nil {
				t.Fatal(err)
			}
			after := readBack(t, f)
			c, _ := ConditionOf(after, 20, "Fusion")
			if level == RepairDestroyed && (after.IsDestroyed() || after.CockpitGone || c != CondOK) {
				t.Errorf("%s after a %s repair: destroyed=%v %s", name, level, after.IsDestroyed(), c)
			}
			if level == RepairCrippled && (!after.Destroyed || after.CockpitGone || after.Damaged || c != CondDestroyed) {
				t.Errorf("%s after a %s repair: flag=%v cockpit=%v %s, want destroyed until the destroyed repair", name, level, after.Destroyed, after.CockpitGone, c)
			}
		}
	}
}

func TestStructureRepairRestoresALostLimbOrEngineHit(t *testing.T) {
	for name, state := range map[string]string{
		"one destroyed arm":   `{"destroyed":false,"locations":{"LA":{"armor":2,"internal":3},"CT":{"armor":1,"internal":1}},"crits":[{"name":"Hand Actuator","loc":"LA","destroyed":1,"hits":0}],"crew":[]}`,
		"one engine hit":      `{"destroyed":false,"locations":{"CT":{"armor":1,"internal":1}},"crits":[{"name":"Fusion Engine","loc":"CT","destroyed":1,"hits":0}],"crew":[]}`,
		"one destroyed torso": `{"destroyed":false,"locations":{"RT":{"armor":6,"internal":5},"RA":{"armor":3,"internal":3}},"crits":[{"name":"Medium Laser","loc":"RT","destroyed":1,"hits":0}],"crew":[]}`,
	} {
		var f map[string]any
		if err := json.Unmarshal([]byte(`{"name":"t","groups":[{"name":"","units":[{"id":"c","unit":"BMBaboon","updatedTs":1,"state":`+state+`}]}]}`), &f); err != nil {
			t.Fatal(err)
		}
		if c, _ := ConditionOf(readBack(t, f), 20, "Fusion"); c != CondStructure {
			t.Fatalf("%s before: %s", name, c)
		}
		if err := ServiceUnitState(f, "c", "repair", RepairStructure, 20, false, time.Now()); err != nil {
			t.Fatal(err)
		}
		st := readBack(t, f)
		if c, _ := ConditionOf(st, 20, "Fusion"); c != CondOK || st.Damaged || len(st.Crits) != 0 || len(st.Locations) != 0 {
			t.Errorf("%s after a structure repair: %s damaged=%v crits=%d %+v", name, c, st.Damaged, len(st.Crits), st.Locations)
		}
	}
}

// koshi is a one-unit force holding a Mist Lynx (25 tons: arms 4, legs 6 internal structure) with this state.
func koshi(t *testing.T, state string) map[string]any {
	t.Helper()
	var f map[string]any
	if err := json.Unmarshal([]byte(`{"name":"t","groups":[{"name":"","units":[{"id":"c","unit":"BMKoshi_E","updatedTs":1,"state":`+state+`}]}]}`), &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// A blown-off leg is a destroyed location whatever its structure: with a destroyed arm it cripples the unit, and only the crippled repair puts it back. The armor
// and structure repairs leave it blown off.
func TestBlownOffLegNeedsTheCrippledRepair(t *testing.T) {
	const state = `{"destroyed":false,"locations":{"LL":{"armor":2,"internal":0,"conditions":["blown-off"]},"RA":{"armor":3,"internal":4},"CT":{"internal":1}},
		"crits":[{"name":"Hip","loc":"LL","slot":0,"destroyed":1,"hits":0},{"name":"Medium Laser","loc":"CT","slot":5,"destroyed":1,"hits":1}],"crew":[]}`
	before := readBack(t, koshi(t, state))
	if c, why := ConditionOf(before, 25, "XL (Clan)"); c != CondCrippled {
		t.Fatalf("a blown-off leg and a destroyed arm cripple the unit: %s (%s)", c, why)
	}
	for _, level := range []string{RepairArmor, RepairStructure} {
		f := koshi(t, state)
		if err := ServiceUnitState(f, "c", "repair", level, 25, true, time.Now()); err != nil {
			t.Fatal(err)
		}
		after := readBack(t, f)
		if c, _ := ConditionOf(after, 25, "XL (Clan)"); c != CondCrippled || !after.Locations["LL"].Gone || after.Locations["LL"].Armor != 0 {
			t.Errorf("a %s repair leaves the leg blown off: %s %+v", level, c, after.Locations)
		}
		if level == RepairStructure && (after.Locations["CT"].Internal != 0 || len(after.Crits) != 1) {
			t.Errorf("the structure repair fixes the surviving torso, not the leg's slots: %+v %+v", after.Locations, after.Crits)
		}
	}
	f := koshi(t, state)
	if err := ServiceUnitState(f, "c", "repair", RepairCrippled, 25, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if after := readBack(t, f); after.Damaged || len(after.Locations) != 0 {
		t.Errorf("the crippled repair restores the leg: %+v", after)
	}
	// A blown-off arm alone doesn't cripple: the structure repair restores it, as it does a destroyed arm.
	f = koshi(t, `{"destroyed":false,"locations":{"LA":{"conditions":[{"key":"flooded"}]}},"crits":[],"crew":[]}`)
	if c, _ := ConditionOf(readBack(t, f), 25, "XL (Clan)"); c != CondStructure {
		t.Fatalf("a flooded arm alone: %s", c)
	}
	if err := ServiceUnitState(f, "c", "repair", RepairStructure, 25, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if after := readBack(t, f); after.Damaged {
		t.Errorf("the structure repair restores a lone flooded arm: %+v", after)
	}
}

// Destroyed equipment on the inventory is damage: the structure repair fixes it (pending destruction too), and a rearm refills the ammunition an inventory item
// used, as it does a bin's.
func TestInventoryDamageAndAmmo(t *testing.T) {
	const state = `{"destroyed":false,"crits":[],"crew":[],
		"inventory":[{"id":"CLERMediumLaser@RA#0","destroyed":true,"states":[{"name":"mode","value":"x"}]},{"id":"Coolant Pod@CT#2","consumed":1}]}`
	hu := readBack(t, koshi(t, state))
	if c, _ := ConditionOf(hu, 25, "XL (Clan)"); c != CondStructure || !hu.Damaged || hu.AmmoBins != 1 {
		t.Fatalf("destroyed equipment is structure-level damage, a used pod ammo: %s %+v", c, hu)
	}
	f := koshi(t, state)
	if err := ServiceUnitState(f, "c", "repair", RepairStructure, 25, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if after := readBack(t, f); after.Damaged || after.AmmoBins != 1 {
		t.Errorf("the repair fixes the laser and leaves the pod for a rearm: %+v", after)
	}
	f = koshi(t, state)
	if err := ServiceUnitState(f, "c", "rearm", "", 25, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if after := readBack(t, f); !after.Damaged || after.AmmoBins != 0 || after.AmmoUsed != 0 {
		t.Errorf("the rearm refills the pod and leaves the laser: %+v", after)
	}
}

// Clearing a critical slot clears its pending hits and every hit timestamp too, so nothing of the damage is left for MekBay's record sheet to show.
func TestClearCritDropsPendingHits(t *testing.T) {
	cm := map[string]any{"name": "Hip", "loc": "LL", "hits": 1.0, "pendingHits": 1.0, "hitTimestamps": []any{1.0}, "pendingHitTimestamps": []any{2.0}, "destroyed": 3.0, "destroying": 3.0, "destroyedTurn": 2.0, "consumed": 4.0}
	clearCrit(cm)
	want := map[string]any{"name": "Hip", "loc": "LL", "hits": 0, "consumed": 4.0}
	if !reflect.DeepEqual(cm, want) {
		t.Errorf("clearCrit = %v, want %v", cm, want)
	}
}
