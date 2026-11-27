package site

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/format"
	"github.com/davidzech/Battletech/internal/hangar"
	"github.com/davidzech/Battletech/internal/mekbay"
)

// Selling units: the price is package hangar (sell.go), removing the unit from the force package mekbay.

// sellUnit is POST /api/hangar/sell: sell the hangar unit named by `unit`. The server works out the price itself from the unit's state. The unit leaves the hangar
// and the warchest is paid in one transaction, so a unit never leaves unpaid and is never paid for twice.
func (s *Server) sellUnit(w http.ResponseWriter, r *http.Request) {
	pt, c, _, ok := s.runRequest(w, r)
	if !ok {
		return
	}
	unitID := r.PostFormValue("unit")
	warchest, value, ok := s.changeHangar(w, r, pt, c, func(force map[string]any, h mekbay.HangarForce) (int, string, error) {
		hu, found := hangarUnit(h, unitID)
		if !found {
			return 0, "", hangarError{http.StatusNotFound, "That unit is no longer in your hangar."}
		}
		cat, known := catalog.ByName(hu.Unit)
		if !known || cat.Type != "Mek" {
			return 0, "", hangarError{http.StatusBadRequest, "Only mechs in the catalog can be sold here."}
		}
		value := hangar.SellValue(cat.BV, hangar.Pristine(hu))
		if !mekbay.RemoveUnitFromForce(force, hu.ID) {
			return 0, "", refused(errors.New("That unit is no longer in your hangar."))
		}
		return value, fmt.Sprintf("Sold %s for %s SP.", catalog.DisplayName(cat), format.Commas(value)), nil
	})
	if ok {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"value":%d,"warchest":%d}`, value, warchest) // what the payment made, whatever other requests did since this one loaded the run
	}
}
