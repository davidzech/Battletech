package hangar

import (
	"math"
	"sort"
	"sync"

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

// SwapOption is one variant an omnimech can be reconfigured to, with the SP that costs.
type SwapOption struct {
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
	// Card is the variant's unit card, shown when its row in the Reconfigure dialog is hovered (the same card as everywhere else, see catalog.CardFor).
	Card *catalog.UnitCard `json:"card,omitempty"`
	// Available is whether the campaign's faction fields this variant in its era (availability_faction and availability_era in Intro.md, the same
	// list the purchase search filters by); the Reconfigure dialog offers only these unless the player unticks the box. A campaign that names no
	// faction and era leaves every variant available. Set by markSwapAvailability, which knows the campaign.
	Available bool `json:"available"`
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

// SwapOptions lists the other variants of an omnimech, cheapest BV first; none for a unit that isn't an omnimech.
func SwapOptions(u catalog.Unit) []SwapOption {
	if u.Type != "Mek" || u.Omni == 0 {
		return nil
	}
	loadOmniVariants()
	var out []SwapOption
	for _, v := range omniVariants[u.Chassis] {
		if v.Name != u.Name {
			name, model := catalog.Names(v)
			out = append(out, SwapOption{Unit: v.Name, Model: model, BV: v.BV, Cost: swapPrice(u, v), Name: name, Class: v.Class, Role: catalog.RoleLabel(v), Tons: v.Tons, Icon: catalog.IconURL(v), MekBayURL: catalog.PageURL(v.Name), Card: catalog.CardFor(v)})
		}
	}
	return out
}
