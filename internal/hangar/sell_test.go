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
	} {
		if Pristine(hu) {
			t.Errorf("%s: should not be pristine", name)
		}
	}
}
