package mekbay

import (
	"encoding/json"
	"errors"
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
// A unit whose cockpit is gone counts as destroyed even without MekBay's destroyed flag (damage written outside MekBay's app), and a destroyed or crippled
// repair, which restores the head and the cockpit, brings it back.
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
			if c, _ := ConditionOf(after, 20, "Fusion"); after.IsDestroyed() || after.CockpitGone || c != CondOK {
				t.Errorf("%s after a %s repair: destroyed=%v %s", name, level, after.IsDestroyed(), c)
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
