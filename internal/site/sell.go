package site

import (
	"fmt"
	"log"
	"net/http"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/format"
	"github.com/davidzech/Battletech/internal/hangar"
	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/storage"
)

// Selling units: the price is package hangar (sell.go), removing the unit from the force package mekbay.

// sellUnit is POST /api/hangar/sell: sell the hangar unit named by `unit`. The server works out the price itself from the unit's state. The unit is taken out of
// the hangar first and the warchest paid after, so a failure in MekBay never pays for a unit the player still has.
func (s *Server) sellUnit(w http.ResponseWriter, r *http.Request) {
	pt, c, run, ok := s.hangarRequest(w, r)
	if !ok {
		return
	}
	h, err := s.mekbay.ReadHangar(r.Context(), run.HangarInstance)
	if err != nil {
		log.Printf("read hangar to sell a unit: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	var hu mekbay.HangarUnit
	found := false
	for _, u := range h.Units {
		if u.ID == r.PostFormValue("unit") {
			hu, found = u, true
		}
	}
	if !found {
		http.Error(w, "That unit is no longer in your hangar.", http.StatusNotFound)
		return
	}
	cat, known := catalog.ByName(hu.Unit)
	if !known || cat.Type != "Mek" {
		http.Error(w, "Only mechs in the catalog can be sold here.", http.StatusBadRequest)
		return
	}
	value := hangar.SellValue(cat.BV, hangar.Pristine(hu))
	after, err := s.mekbay.SellUnit(r.Context(), mekbay.SellRequest{OwnerUID: mekbay.AccountUID(currentSeed(r), mekbay.SlotHangar), Instance: run.HangarInstance, UnitID: hu.ID})
	if err != nil {
		log.Printf("sell a unit in MekBay: %v", err) // never log the owner UID
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.cacheHangar(r.Context(), pt.ID, c.Slug, run.HangarInstance, after) // the page that reloads next shows the unit gone at once
	if err := s.store.RefundWarchest(r.Context(), pt.ID, c.Slug, value); err != nil {
		log.Printf("could not pay %d SP for a sold unit: %v", value, err)
		http.Error(w, "The unit was sold but the warchest could not be paid. Set the funds under Advanced Management.", http.StatusInternalServerError)
		return
	}
	s.logHistory(r, pt.ID, c.Slug, storage.HistoryHangar, value, fmt.Sprintf("Sold %s for %s SP.", catalog.DisplayName(cat), format.Commas(value)))
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"value":%d,"warchest":%d}`, value, run.Warchest+value)
}
