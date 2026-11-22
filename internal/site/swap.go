package site

import (
	"context"
	"errors"
	"log"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"

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

// swapOption is one variant an omnimech can be reconfigured to, with the SP that costs.
type swapOption struct {
	Unit  string `json:"unit"`  // MekBay unit ID of the variant, e.g. BMMadCat_H
	Model string `json:"model"` // e.g. "H"
	BV    int    `json:"bv"`
	Cost  int    `json:"cost"` // SP to swap the unit to this variant
	// What the page needs to draw the variant as a mech row, like a hangar unit.
	Name      string  `json:"name"`
	Class     string  `json:"class"`
	Role      string  `json:"role"`
	Tons      float64 `json:"tons"`
	Icon      string  `json:"icon"`
	MekBayURL string  `json:"mekbayUrl"`
	// Available is whether the campaign's faction fields this variant in its era (availability_faction and availability_era in Intro.md, the same
	// list the purchase search filters by); the Reconfigure dialog offers only these unless the player unticks the box. A campaign that names no
	// faction and era leaves every variant available. Set by markSwapAvailability, which knows the campaign.
	Available bool `json:"available"`
}

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

var (
	omniOnce     sync.Once
	omniVariants map[string][]catalog.Unit // omnimech variants by chassis
)

func loadOmniVariants() {
	omniOnce.Do(func() {
		omniVariants = map[string][]catalog.Unit{}
		for _, u := range catalog.All() {
			if u.Type == "Mek" && u.Omni != 0 {
				omniVariants[u.Chassis] = append(omniVariants[u.Chassis], u)
			}
		}
		for _, list := range omniVariants {
			sort.Slice(list, func(i, j int) bool {
				if list[i].BV != list[j].BV {
					return list[i].BV < list[j].BV
				}
				return list[i].Name < list[j].Name
			})
		}
	})
}

// swapPrice is the SP to reconfigure an omnimech from one variant to another: half the tonnage (rounded up) and half the BV gained (rounded down).
func swapPrice(from, to catalog.Unit) int {
	cost := int(math.Ceil(from.Tons / 2))
	if gain := to.BV - from.BV; gain > 0 {
		cost += gain / 2
	}
	return cost
}

// swapOptions lists the other variants of an omnimech, cheapest BV first; none for a unit that isn't an omnimech.
func swapOptions(u catalog.Unit) []swapOption {
	if u.Type != "Mek" || u.Omni == 0 {
		return nil
	}
	loadOmniVariants()
	var out []swapOption
	for _, v := range omniVariants[u.Chassis] {
		if v.Name != u.Name {
			name, model := catalog.Names(v)
			out = append(out, swapOption{Unit: v.Name, Model: model, BV: v.BV, Cost: swapPrice(u, v), Name: name, Class: v.Class, Role: catalog.RoleLabel(v), Tons: v.Tons, Icon: catalog.IconURL(v), MekBayURL: catalog.PageURL(v.Name)})
		}
	}
	return out
}

// swapUnitState reconfigures the unit with this ID inside a force in MekBay's save format: its design becomes newName and its state is reset (a fresh unit has no
// damage, no critical hits and full ammunition, and the old variant's slots mean nothing to the new one).
func swapUnitState(force map[string]any, unitID, newName string, now time.Time) error {
	groups, _ := force["groups"].([]any)
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		units, _ := gm["units"].([]any)
		for _, u := range units {
			um, _ := u.(map[string]any)
			if um == nil || um["id"] != unitID {
				continue
			}
			pristineUnit(um, now)
			state, _ := um["state"].(map[string]any)
			state["crits"] = []any{}
			um["unit"] = newName
			return nil
		}
	}
	return errors.New("that unit is no longer in your hangar force")
}

// SwapRequest asks MekBay to reconfigure a unit of the site's copy of the hangar.
type SwapRequest struct {
	OwnerUID string
	Instance string
	UnitID   string // the unit's own ID inside the force
	To       string // MekBay unit ID of the variant
}

// SwapUnit reconfigures a unit in the hangar force. It needs the owner's UID, because MekBay only accepts the owner's saves.
func (c *MekBayClient) SwapUnit(ctx context.Context, req SwapRequest) error {
	return c.editForce(ctx, req.OwnerUID, req.Instance, func(force map[string]any, now time.Time) error {
		return swapUnitState(force, req.UnitID, req.To, now)
	})
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
	var hu HangarUnit
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
	if !unitPristine(hu) {
		http.Error(w, "An omnimech has to be fully repaired and rearmed before it can be reconfigured.", http.StatusConflict)
		return
	}
	to := r.PostFormValue("to")
	var option *swapOption
	for _, o := range swapOptions(cat) {
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
		return s.mekbay.SwapUnit(r.Context(), SwapRequest{OwnerUID: accountUID(currentSeed(r), SlotHangar), Instance: run.HangarInstance, UnitID: hu.ID, To: to})
	})
}
