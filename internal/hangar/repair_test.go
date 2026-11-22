package hangar

import (
	"strings"
	"testing"

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
		{"truly destroyed", loc(map[string]mekbay.LocationLoss{"CT": {Internal: 6}}), mekbay.CondTruly, ""},
	}
	for _, c := range cases {
		if got := strings.Join(RepairOptions(c.hu, c.cond, 20), ","); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
