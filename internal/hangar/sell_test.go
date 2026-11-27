package hangar

import (
	"testing"

	"github.com/davidzech/Battletech/internal/mekbay"
)

func TestSellValue(t *testing.T) {
	for _, c := range []struct {
		bv       int
		pristine bool
		want     int
	}{
		{1000, true, 500}, {1000, false, 250}, {1001, true, 500}, {1001, false, 250}, {1003, false, 250}, {7, true, 3}, {7, false, 1}, {1, false, 0},
	} {
		if got := SellValue(c.bv, c.pristine); got != c.want {
			t.Errorf("sellValue(%d, %v) = %d, want %d", c.bv, c.pristine, got, c.want)
		}
	}
	if !Pristine(mekbay.HangarUnit{}) {
		t.Error("an untouched unit is pristine")
	}
	for name, hu := range map[string]mekbay.HangarUnit{
		"damaged": {Damaged: true}, "destroyed": {Destroyed: true}, "spent ammo": {AmmoBins: 1, AmmoUsed: 4},
		"lost armor": {Locations: map[string]mekbay.LocationLoss{"CT": {Armor: 1}}}, "a crit": {Crits: []mekbay.CritDamage{{}}},
		"a blown-off arm": {Locations: map[string]mekbay.LocationLoss{"LA": {Gone: true}}},
	} {
		if Pristine(hu) {
			t.Errorf("%s: should not be pristine", name)
		}
	}
}

// Destroyed equipment on the inventory, or ammunition an inventory item used, keeps a unit from being pristine, as a crit or a spent bin does.
func TestInventoryStateIsNotPristine(t *testing.T) {
	for name, inv := range map[string]string{
		"destroyed equipment": `[{"id":"CLERMediumLaser@RA#0","destroyed":true}]`,
		"used equipment":      `[{"id":"Coolant Pod@CT#2","consumed":1}]`,
	} {
		h, err := mekbay.ParseForce([]byte(`{"groups":[{"name":"","units":[{"id":"u","unit":"BMKoshi_E","state":{"inventory":` + inv + `,"crits":[],"crew":[]}}]}]}`))
		if err != nil || len(h.Units) != 1 {
			t.Fatalf("%s: %v", name, err)
		}
		if Pristine(h.Units[0]) {
			t.Errorf("%s: should not be pristine", name)
		}
	}
}
