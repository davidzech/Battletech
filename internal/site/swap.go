package site

import (
	"fmt"
	"log"
	"net/http"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/format"
	"github.com/davidzech/Battletech/internal/hangar"
	"github.com/davidzech/Battletech/internal/mekbay"
)

// Reconfiguring omnimechs: the price and the variants offered are package hangar (swap.go), the edit to the force package mekbay.

// markSwapAvailability marks each unit's reconfigure options as available or not to the campaign's faction in its era.
func markSwapAvailability(views []hangarUnitView, c *content.Campaign) {
	var available map[string]bool
	if c != nil && c.AvailabilityFaction != "" && c.AvailabilityEra != "" {
		available = catalog.Available(c.AvailabilityFaction, c.AvailabilityEra)
	}
	for i := range views {
		for j := range views[i].SwapOptions {
			o := &views[i].SwapOptions[j]
			o.Available = available == nil || available[o.Unit]
		}
	}
}

// swapUnit is POST /api/hangar/swap: reconfigure the hangar unit named by `unit` to the variant `to`. The server works the price out itself, refuses a unit that isn't
// fully repaired and rearmed or a variant that isn't one of its chassis's, and charges the warchest first (refunded if MekBay fails).
func (s *Server) swapUnit(w http.ResponseWriter, r *http.Request) {
	pt, c, run, ok := s.hangarRequest(w, r)
	if !ok {
		return
	}
	h, err := s.mekbay.ReadHangar(r.Context(), run.HangarInstance)
	if err != nil {
		log.Printf("read hangar to swap a unit: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	hu, found := hangarUnit(h, r.PostFormValue("unit"))
	if !found {
		http.Error(w, "That unit is no longer in your hangar.", http.StatusNotFound)
		return
	}
	cat, known := catalog.ByName(hu.Unit)
	if !known || cat.Type != "Mek" || cat.Omni == 0 {
		http.Error(w, "Only omnimechs can be reconfigured.", http.StatusBadRequest)
		return
	}
	if !hangar.Pristine(hu) {
		http.Error(w, "An omnimech has to be fully repaired and rearmed before it can be reconfigured.", http.StatusConflict)
		return
	}
	to := r.PostFormValue("to")
	var option *hangar.SwapOption
	for _, o := range hangar.SwapOptions(cat) {
		if o.Unit == to {
			o := o
			option = &o
		}
	}
	if option == nil {
		http.Error(w, "That isn't a variant this omnimech can be reconfigured to.", http.StatusBadRequest)
		return
	}
	cost := option.Cost
	history := fmt.Sprintf("Reconfigured %s to %s for %s SP.", catalog.DisplayName(cat), option.Model, format.Commas(cost))
	s.payForHangarChange(w, r, pt, c, run, cost, "swap a unit", history, func() (mekbay.HangarForce, error) {
		return s.mekbay.SwapUnit(r.Context(), mekbay.SwapRequest{OwnerUID: mekbay.AccountUID(currentSeed(r), mekbay.SlotHangar), Instance: run.HangarInstance, UnitID: hu.ID, To: to})
	})
}
