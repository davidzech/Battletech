package site

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/format"
	"github.com/davidzech/Battletech/internal/hangar"
	"github.com/davidzech/Battletech/internal/mekbay"
)

// Buying new units for the hangar. The price is the unit's base BV in SP (this campaign counts BV as SP, Hot Spots: Draconis Reach, SP Activity
// Cost Table: Purchase Unit = Battle Value). A purchase adds each unit, undamaged and with a standard 4/5 pilot, to the "Hangar" group of the
// campaign's hangar, and is paid from the warchest.

// purchaseUnits is POST /api/hangar/purchase: buy the units named by repeated `unit` fields, paid from the warchest at their base BV, and add
// them to the hangar, both in one transaction.
func (s *Server) purchaseUnits(w http.ResponseWriter, r *http.Request) {
	pt, c, _, ok := s.runRequest(w, r)
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
	warchest, _, ok := s.changeHangar(w, r, pt, c, func(force map[string]any, _ mekbay.HangarForce) (int, string, error) {
		if err := mekbay.AddUnitsToForce(force, ids, time.Now()); err != nil {
			return 0, "", hangarError{http.StatusBadRequest, err.Error()}
		}
		return -cost, history, nil
	})
	if ok {
		writeCharge(w, cost, warchest)
	}
}
