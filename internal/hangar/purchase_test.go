package hangar

import (
	"testing"

	"github.com/davidzech/Battletech/internal/catalog"
)

func TestPurchaseCost(t *testing.T) {
	madCat, _ := catalog.ByName("BMMadCat_H")
	koshi, _ := catalog.ByName("BMKoshi_E")
	if got, err := PurchaseCost([]string{"BMMadCat_H", "BMKoshi_E"}); err != nil || got != madCat.BV+koshi.BV || got == 0 {
		t.Errorf("cost of two mechs: %d %v (want %d)", got, err, madCat.BV+koshi.BV)
	}
	for name, ids := range map[string][]string{
		"nothing":     nil,
		"not in list": {"BMNoSuchMech"},
		"not a mech":  {"BMMadCat_H", "BMNoSuchMech"},
		"too many":    {"BMMadCat_H", "BMMadCat_H", "BMMadCat_H", "BMMadCat_H", "BMMadCat_H", "BMMadCat_H", "BMMadCat_H", "BMMadCat_H", "BMMadCat_H", "BMMadCat_H", "BMMadCat_H"},
	} {
		if _, err := PurchaseCost(ids); err == nil {
			t.Errorf("%s should be refused", name)
		}
	}
}
