package hangar

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/davidzech/Battletech/internal/mekbay"
)

func TestRepairCostsFollowTheSPActivityCostTable(t *testing.T) {
	cases := []struct {
		level string
		tons  float64
		tech  string
		want  int
	}{
		{mekbay.RepairArmor, 20, "Inner Sphere", 10}, {mekbay.RepairStructure, 20, "Inner Sphere", 40},
		{mekbay.RepairCrippled, 20, "Inner Sphere", 60}, {mekbay.RepairDestroyed, 20, "Inner Sphere", 100},
		{mekbay.RepairArmor, 20, "Clan", 15}, {mekbay.RepairStructure, 20, "Clan", 60}, {mekbay.RepairCrippled, 20, "Clan", 90}, {mekbay.RepairDestroyed, 20, "Clan", 150},
		{mekbay.RepairArmor, 25, "Mixed", 19},        // 12.5 x 1.5 = 18.75, rounded up
		{mekbay.RepairArmor, 25, "Inner Sphere", 13}, // 12.5 rounded up
		{mekbay.RepairDestroyed, 100, "Clan", 750},
	}
	for _, c := range cases {
		if got := RepairCost(c.level, c.tons, c.tech); got != c.want {
			t.Errorf("%s %vt %s: %d, want %d", c.level, c.tons, c.tech, got, c.want)
		}
	}
	if RearmCost(3) != 30 {
		t.Errorf("rearm of three bins: %d", RearmCost(3))
	}
}

func TestRepairOptions(t *testing.T) {
	loc := func(m map[string]mekbay.LocationLoss) mekbay.HangarUnit { return mekbay.HangarUnit{Locations: m} }
	cases := []struct {
		name string
		hu   mekbay.HangarUnit
		cond mekbay.Condition
		want string
	}{
		{"undamaged", mekbay.HangarUnit{}, mekbay.CondOK, ""},
		{"armor only", loc(map[string]mekbay.LocationLoss{"CT": {Armor: 4}}), mekbay.CondArmor, "armor"},
		{"structure only, no armor lost", loc(map[string]mekbay.LocationLoss{"CT": {Internal: 2}}), mekbay.CondStructure, "structure"},
		{"armor and structure", loc(map[string]mekbay.LocationLoss{"CT": {Armor: 4, Internal: 2}}), mekbay.CondStructure, "armor,structure"},
		{"a crit in a living location is structure-level", mekbay.HangarUnit{Crits: []mekbay.CritDamage{{Loc: "RT"}}}, mekbay.CondStructure, "structure"},
		{"crippled by destroyed limbs, armor also lost", loc(map[string]mekbay.LocationLoss{"LL": {Internal: 4}, "RA": {Armor: 5, Internal: 3}}), mekbay.CondCrippled, "armor,crippled"},
		{"crippled with surviving structure damage too", loc(map[string]mekbay.LocationLoss{"LL": {Internal: 4}, "RA": {Internal: 3}, "CT": {Internal: 1}}), mekbay.CondCrippled, "structure,crippled"},
		{"crippled by engine hits only", mekbay.HangarUnit{EngineHits: 2, Crits: []mekbay.CritDamage{{Loc: "CT", Engine: true}}}, mekbay.CondCrippled, "crippled"},
		{"destroyed", mekbay.HangarUnit{Destroyed: true, Locations: map[string]mekbay.LocationLoss{"HD": {Armor: 1, Internal: 3}}}, mekbay.CondDestroyed, "armor,crippled,destroyed"},
		{"one destroyed arm, not crippled", loc(map[string]mekbay.LocationLoss{"LA": {Armor: 2, Internal: 3}, "CT": {Internal: 1}}), mekbay.CondStructure, "armor,structure"},
		{"one destroyed leg and nothing else", loc(map[string]mekbay.LocationLoss{"LL": {Internal: 4}}), mekbay.CondStructure, "structure"},
		{"one destroyed side torso that did not cripple (a fusion engine that passed its check)", loc(map[string]mekbay.LocationLoss{"RT": {Internal: 5}}), mekbay.CondStructure, "structure"},
		{"a destroyed unit with one limb gone still needs the destroyed level", mekbay.HangarUnit{Destroyed: true, Locations: map[string]mekbay.LocationLoss{"LA": {Internal: 3}}}, mekbay.CondDestroyed, "crippled,destroyed"},
		{"a crit in a destroyed location goes with the location", mekbay.HangarUnit{Locations: map[string]mekbay.LocationLoss{"LL": {Internal: 4}}, Crits: []mekbay.CritDamage{{Loc: "LL"}}}, mekbay.CondStructure, "structure"},
		{"one engine hit", mekbay.HangarUnit{EngineHits: 1, Crits: []mekbay.CritDamage{{Loc: "CT", Engine: true}}}, mekbay.CondStructure, "structure"},
		{"one engine hit and structure damage", mekbay.HangarUnit{EngineHits: 1, Crits: []mekbay.CritDamage{{Loc: "CT", Engine: true}}, Locations: map[string]mekbay.LocationLoss{"CT": {Internal: 1}}}, mekbay.CondStructure, "structure"},
		{"truly destroyed", loc(map[string]mekbay.LocationLoss{"CT": {Internal: 6}}), mekbay.CondTruly, ""},
	}
	for _, c := range cases {
		if got := strings.Join(RepairOptions(c.hu, c.cond, 20), ","); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// Whatever a unit's damage, short of truly destroyed, some offered repair must be able to fix all of it: the conditions here come from ConditionOf,
// as the site works them out, not written by hand.
func TestEveryDamageHasARepair(t *testing.T) {
	cases := map[string]mekbay.HangarUnit{
		"critical hit only":   {Damaged: true, Crits: []mekbay.CritDamage{{Loc: "RT"}}},
		"engine hit only":     {Damaged: true, EngineHits: 1, Crits: []mekbay.CritDamage{{Loc: "CT", Engine: true}}},
		"one destroyed arm":   {Damaged: true, Locations: map[string]mekbay.LocationLoss{"LA": {Internal: 3}}},
		"one destroyed leg":   {Damaged: true, Locations: map[string]mekbay.LocationLoss{"RL": {Armor: 4, Internal: 4}}},
		"destroyed head only": {Damaged: true, Locations: map[string]mekbay.LocationLoss{"HD": {Internal: 3}}},
		"one destroyed torso": {Damaged: true, Locations: map[string]mekbay.LocationLoss{"LT": {Internal: 5}}},
	}
	for name, hu := range cases {
		cond, note := mekbay.ConditionOf(hu, 20, "Fusion")
		if cond == mekbay.CondOK {
			t.Errorf("%s: reported as undamaged (%s)", name, note)
		}
		if len(RepairOptions(hu, cond, 20)) == 0 {
			t.Errorf("%s: no repair offered (condition %s)", name, cond)
		}
	}
}

// hangarAfter writes one battle unit (design, state as JSON) back into a hangar holding it, as completing a mission does, and returns the hangar and the unit as
// the site reads it.
func hangarAfter(t *testing.T, design, state string) (map[string]any, mekbay.HangarUnit) {
	t.Helper()
	var st map[string]any
	if err := json.Unmarshal([]byte(state), &st); err != nil {
		t.Fatal(err)
	}
	unit := func(s map[string]any) map[string]any { return map[string]any{"id": "u", "unit": design, "state": s} }
	force := map[string]any{"name": "H", "groups": []any{map[string]any{"name": "", "units": []any{unit(map[string]any{"crew": []any{}})}}}}
	mekbay.CommitState(force, map[string]map[string]any{"u": unit(st)})
	return force, readUnit(t, force)
}

func readUnit(t *testing.T, force map[string]any) mekbay.HangarUnit {
	t.Helper()
	b, _ := json.Marshal(force)
	h, err := mekbay.ParseForce(b)
	if err != nil || len(h.Units) != 1 {
		t.Fatalf("parse: %v %+v", err, h)
	}
	return h.Units[0]
}

// The repairs offered for damage written back from a battle that ended mid-phase, its damage still pending in MekBay.
func TestRepairOptionsAfterWriteBack(t *testing.T) {
	// A Timber Wolf (75 tons) whose center torso went in the pending damage is truly destroyed: no repair at all.
	_, hu := hangarAfter(t, "BMMadCat_H", `{"locations":{"CT":{"armor":4,"internal":2,"pendingInternal":21}},"crits":[{"name":"Engine","loc":"CT","destroying":1}],"crew":[]}`)
	if c, _ := mekbay.ConditionOf(hu, 75, "XL (Clan)"); c != mekbay.CondTruly || len(RepairOptions(hu, c, 75)) != 0 {
		t.Errorf("pending center torso loss: %s, options %v", c, RepairOptions(hu, c, 75))
	}
	// A Mist Lynx (25 tons) that lost a leg in the pending damage and an arm before: crippled, and an armor repair leaves it so.
	force, hu := hangarAfter(t, "BMKoshi_E", `{"locations":{"LL":{"armor":3,"internal":2,"pendingInternal":4},"RA":{"internal":4}},"crits":[],"crew":[]}`)
	c, _ := mekbay.ConditionOf(hu, 25, "XL (Clan)")
	if got := strings.Join(RepairOptions(hu, c, 25), ","); c != mekbay.CondCrippled || got != "armor,crippled" {
		t.Fatalf("pending leg loss: %s, options %s", c, got)
	}
	if err := mekbay.ServiceUnitState(force, "u", "repair", mekbay.RepairArmor, 25, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if c, _ := mekbay.ConditionOf(readUnit(t, force), 25, "XL (Clan)"); c != mekbay.CondCrippled {
		t.Errorf("an armor repair doesn't fix a crippled unit: %s", c)
	}
	// A leg blown off (pending when the battle ended) and an arm lost: crippled, and only the crippled level restores the leg.
	_, hu = hangarAfter(t, "BMKoshi_E", `{"locations":{"RL":{"conditions":[{"key":"blown-off","pending":true}]},"LA":{"internal":4}},"crits":[],"crew":[]}`)
	if c, _ := mekbay.ConditionOf(hu, 25, "XL (Clan)"); c != mekbay.CondCrippled || strings.Join(RepairOptions(hu, c, 25), ",") != "crippled" {
		t.Errorf("blown-off leg: %s, options %v", c, RepairOptions(hu, c, 25))
	}
}

// A unit destroyed only by losing its head, with no destroyed flag (a hangar written before the write-back set it), needs the destroyed repair like one MekBay
// flagged.
func TestCockpitLossNeedsTheDestroyedRepair(t *testing.T) {
	force := map[string]any{"name": "H", "groups": []any{map[string]any{"name": "", "units": []any{map[string]any{"id": "u", "unit": "BMKoshi_E",
		"state": map[string]any{"destroyed": false, "locations": map[string]any{"HD": map[string]any{"armor": 9.0, "internal": 3.0}}, "crits": []any{}, "crew": []any{}}}}}}}
	hu := readUnit(t, force)
	c, _ := mekbay.ConditionOf(hu, 25, "XL (Clan)")
	if got := strings.Join(RepairOptions(hu, c, 25), ","); c != mekbay.CondDestroyed || got != "armor,crippled,destroyed" {
		t.Fatalf("head lost: %s, options %s", c, got)
	}
	if err := mekbay.ServiceUnitState(force, "u", "repair", mekbay.RepairCrippled, 25, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	hu = readUnit(t, force)
	if c, _ := mekbay.ConditionOf(hu, 25, "XL (Clan)"); c != mekbay.CondDestroyed || strings.Join(RepairOptions(hu, c, 25), ",") != "crippled,destroyed" {
		t.Errorf("after the crippled repair it is still destroyed: %s %v", c, RepairOptions(hu, c, 25))
	}
}
