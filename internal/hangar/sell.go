package hangar

import (
	"github.com/davidzech/Battletech/internal/mekbay"
)

// Selling units. A unit sells for half its base BV in SP (this campaign counts BV as SP, as purchasing does), but only a unit that is fully repaired and
// rearmed gets that: any damage or spent ammunition halves the price again, rounded down, so a worn unit fetches about a quarter of its BV. A truly destroyed
// mech (see CondTruly) can't be repaired, so selling it is the only thing left to do with it, and it always sells at the reduced price. The sale takes the unit
// out of the hangar force (crew and all) and pays the warchest.

// Pristine reports whether a unit is fully repaired and rearmed: no recorded damage, not destroyed, no spent ammunition.
func Pristine(hu mekbay.HangarUnit) bool {
	return !hu.Damaged && !hu.IsDestroyed() && hu.AmmoUsed == 0 && hu.AmmoBins == 0 && len(hu.Locations) == 0 && len(hu.Crits) == 0
}

// SellValue is the SP a unit with this base BV sells for: half its BV when pristine, otherwise half of that, rounding down each time.
func SellValue(bv int, pristine bool) int {
	v := bv / 2
	if !pristine {
		v /= 2
	}
	return v
}
