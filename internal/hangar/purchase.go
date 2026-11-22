package hangar

import (
	"errors"
	"fmt"

	"github.com/davidzech/Battletech/internal/catalog"
)

// maxPurchase is how many units one purchase may add, so a mistake can't empty the warchest into dozens of units at once.
const maxPurchase = 10

// PurchaseCost is the SP price of a list of units, or an error naming the first one that can't be bought (not in the catalog, or not a mech).
func PurchaseCost(ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, errors.New("Choose at least one unit to buy.")
	}
	if len(ids) > maxPurchase {
		return 0, fmt.Errorf("You can buy at most %d units at a time.", maxPurchase)
	}
	total := 0
	for _, id := range ids {
		u, ok := catalog.ByName(id)
		if !ok || u.Type != "Mek" {
			return 0, fmt.Errorf("%q is not a mech in the unit list.", id)
		}
		total += u.BV
	}
	return total, nil
}
