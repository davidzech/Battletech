package hangar

import (
	"github.com/davidzech/Battletech/internal/mekbay"
)

// Pristine reports whether a unit is fully repaired and rearmed: no recorded damage, not destroyed, no spent ammunition.
func Pristine(hu mekbay.HangarUnit) bool {
	return !hu.Damaged && !hu.Destroyed && hu.AmmoUsed == 0 && hu.AmmoBins == 0 && len(hu.Locations) == 0 && len(hu.Crits) == 0
}

// SellValue is the SP a unit with this base BV sells for: half its BV when pristine, otherwise half of that, rounding down each time.
func SellValue(bv int, pristine bool) int {
	v := bv / 2
	if !pristine {
		v /= 2
	}
	return v
}
