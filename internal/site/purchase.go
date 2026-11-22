package site

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/davidzech/Battletech/internal/mekbay"

	"github.com/davidzech/Battletech/internal/catalog"
)

// Buying new units for the hangar. The price is the unit's base BV in SP (this campaign counts BV as SP, Hot Spots: Draconis Reach, SP Activity
// Cost Table: Purchase Unit = Battle Value). A purchase adds each unit, undamaged and with a standard 4/5 pilot, to the "Hangar" group of the
// player's force in MekBay, and is paid from the warchest.

// maxPurchase is how many units one purchase may add, so a mistake can't empty the warchest into dozens of units at once.
const maxPurchase = 10

// purchaseCost is the SP price of a list of units, or an error naming the first one that can't be bought (not in the catalog, or not a mech).
func purchaseCost(ids []string) (int, error) {
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

// purchaseUnits is POST /api/hangar/purchase: buy the units named by repeated `unit` fields, paid from the warchest at their base BV, and add
// them to the hangar in MekBay. It needs modify mode. The warchest is charged first and refunded if MekBay fails.
func (s *Server) purchaseUnits(w http.ResponseWriter, r *http.Request) {
	pt, c, run, ok := s.hangarRequest(w, r)
	if !ok {
		return
	}
	ids := r.PostForm["unit"]
	cost, err := purchaseCost(ids)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.payForHangarChange(w, r, pt, c, run, cost, "buy units", func() error {
		return s.mekbay.PurchaseUnits(r.Context(), mekbay.PurchaseRequest{OwnerUID: mekbay.AccountUID(currentSeed(r), mekbay.SlotHangar), Instance: run.HangarInstance, Units: ids})
	})
}
