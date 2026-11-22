package hangar

import (
	"math"
	"sort"
	"sync"

	"github.com/davidzech/Battletech/internal/catalog"
)

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
			out = append(out, SwapOption{Unit: v.Name, Model: model, BV: v.BV, Cost: swapPrice(u, v), Name: name, Class: v.Class, Role: catalog.RoleLabel(v), Tons: v.Tons, Icon: catalog.IconURL(v), MekBayURL: catalog.PageURL(v.Name)})
		}
	}
	return out
}
