package site

import (
	"log"
	"net/http"

	"github.com/davidzech/Battletech/internal/hangar"

	"github.com/davidzech/Battletech/internal/mekbay"

	"github.com/davidzech/Battletech/internal/catalog"
)

// Reconfiguring omnimechs. The SP Activity Cost Table prices it at half the unit's tonnage, but in this campaign BV is SP, so that alone would make a cheap variant
// a bargain way to get a better one (and, sold, a way to farm SP). A swap therefore costs half the tonnage plus half the BV gained; swapping to a variant worth less
// costs only the half tonnage and refunds nothing. That is still cheaper than selling the unit and buying the other variant (which costs the new BV minus half the
// old one), so being an omnimech is a real advantage, and swapping up then selling a clean unit always loses the half tonnage, so it can't be farmed.
//
// The unit has to be fully repaired and rearmed first, because the old variant's critical slots and ammunition don't map onto the new one's, and only omnimechs can
// be swapped, and only to variants of their own chassis. The swap replaces the unit's design in the hangar force with a pristine copy of the new one. Crews are a
// per-scenario matter (assigned when deploying), so nothing about the crew is carried over.

// markSwapAvailability marks each unit's reconfigure options as available or not to the campaign's faction in its era.
func markSwapAvailability(views []hangarUnitView, c *Campaign) {
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
	s.payForHangarChange(w, r, pt, c, run, cost, "swap a unit", func() error {
		return s.mekbay.SwapUnit(r.Context(), mekbay.SwapRequest{OwnerUID: mekbay.AccountUID(currentSeed(r), mekbay.SlotHangar), Instance: run.HangarInstance, UnitID: hu.ID, To: to})
	})
}
