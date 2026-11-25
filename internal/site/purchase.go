package site

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/format"
	"github.com/davidzech/Battletech/internal/hangar"
	"github.com/davidzech/Battletech/internal/mekbay"
)

// Buying new units for the hangar. The price is the unit's base BV in SP (this campaign counts BV as SP, Hot Spots: Draconis Reach, SP Activity
// Cost Table: Purchase Unit = Battle Value). A purchase adds each unit, undamaged and with a standard 4/5 pilot, to the "Hangar" group of the
// player's force in MekBay, and is paid from the warchest.

// purchaseUnits is POST /api/hangar/purchase: buy the units named by repeated `unit` fields, paid from the warchest at their base BV, and add
// them to the hangar in MekBay. It needs modify mode. The warchest is charged first and refunded if MekBay fails.
func (s *Server) purchaseUnits(w http.ResponseWriter, r *http.Request) {
	pt, c, run, ok := s.hangarRequest(w, r)
	if !ok {
		return
	}
	ids := r.PostForm["unit"]
	cost, err := hangar.PurchaseCost(ids)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if u, ok := catalog.ByName(id); ok {
			names = append(names, catalog.DisplayName(u))
		}
	}
	history := fmt.Sprintf("Bought %s for %s SP.", strings.Join(names, ", "), format.Commas(cost))
	s.payForHangarChange(w, r, pt, c, run, cost, "buy units", history, func() (mekbay.HangarForce, error) {
		return s.mekbay.PurchaseUnits(r.Context(), mekbay.PurchaseRequest{OwnerUID: mekbay.AccountUID(currentSeed(r), mekbay.SlotHangar), Instance: run.HangarInstance, Units: ids})
	})
}
