package site

import (
	"fmt"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/hangar"
	"github.com/davidzech/Battletech/internal/mekbay"
)

// How a unit is shown: its name (the Clan name first), role label and mech-row view, for every list of mechs on the site.

// hangarUnitView is one hangar unit as the deploy popup shows it.
type hangarUnitView struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`  // the Clan name when the design has one
	Model      string  `json:"model"` // e.g. "Prime"
	Icon       string  `json:"icon"`  // URL of the unit's icon
	Class      string  `json:"class"`
	Role       string  `json:"role"`
	Tons       float64 `json:"tons"`
	BV         int     `json:"bv"` // base battle value; pilot skills never change it in this campaign
	Pilot      string  `json:"pilot"`
	PilotDead  bool    `json:"pilotDead"` // the crew is dead: its own record says so, and in a battle's forces also a gone cockpit (see battleViews)
	Gunnery    int     `json:"gunnery"`
	Piloting   int     `json:"piloting"`
	Damaged    bool    `json:"damaged"`
	Destroyed  bool    `json:"destroyed"`
	Unknown    bool    `json:"unknown"` // not in the unit catalog, so its BV can't be worked out
	InDeployed bool    `json:"inDeployed"`
	// MekBayURL is the unit's own page on mekbay.com (its sheet, not the player's force), "" when the unit has no MekBay ID.
	MekBayURL string `json:"mekbayUrl"`
	// Unit is the MekBay unit ID (e.g. BMMadCat_H); set on unit search results, which the purchase buys by.
	Unit string `json:"unit,omitempty"`
	// Condition is the worst state the unit is in (see condition.go): ok, armor, structure, crippled or destroyed, with a note saying why. AmmoExpended
	// is separate: a unit can be in any condition and need rearming too.
	Condition     string `json:"condition"`
	ConditionNote string `json:"conditionNote"`
	AmmoExpended  bool   `json:"ammoExpended"`
	// Servicing, from the SP Activity Cost Table (see service.go): RepairLevels are the repairs that would fix something (empty when it needs none or
	// is truly destroyed), RepairCosts the SP of every level, RearmCost the SP to refill spent ammunition.
	RepairLevels []string       `json:"repairLevels"`
	RepairCosts  map[string]int `json:"repairCosts,omitempty"`
	RearmCost    int            `json:"rearmCost"`
	// AmmoBreakdown says what a rearm would refill: tons of each ammo type and the shots used out of the shots those tons hold.
	AmmoBreakdown []hangar.AmmoLine `json:"ammoBreakdown"`
	// SellValue is the SP the unit sells for (see sell.go), and SellFull whether that is the full price, which only a fully repaired and rearmed unit gets.
	SellValue int  `json:"sellValue"`
	SellFull  bool `json:"sellFull"`
	// SwapOptions are the other variants an omnimech can be reconfigured to, with their prices (see swap.go); empty for any other unit.
	SwapOptions []hangar.SwapOption `json:"swapOptions,omitempty"`
	// Card is the unit's catalog card, the same as a scenario's unit links show (catalog.CardFor), for the overview's hover card; nil for an unknown unit.
	Card *catalog.UnitCard `json:"card,omitempty"`
}

// deployedGroup is the hangar group whose units the Deploy wizard gives Player 1 to start with (a group the player may have made in MekBay).
const deployedGroup = "Deployed"

// hangarViews describes every unit of a hangar for the deploy popup.
func hangarViews(h mekbay.HangarForce) []hangarUnitView {
	views := make([]hangarUnitView, 0, len(h.Units))
	for _, hu := range h.Units {
		v := hangarUnitView{MekBayURL: catalog.PageURL(hu.Unit), ID: hu.ID, Pilot: hu.Pilot, PilotDead: hu.CrewDead, Gunnery: hu.Gunnery, Piloting: hu.Piloting, Damaged: hu.Damaged, Destroyed: hu.Destroyed, InDeployed: mekbay.SameGroupName(hu.Group, deployedGroup)}
		u, known := catalog.ByName(hu.Unit)
		if known {
			v.Name, v.Model = catalog.Names(u)
			v.Class, v.Role, v.Tons, v.Icon = u.Class, catalog.RoleLabel(u), u.Tons, catalog.IconURL(u)
			v.BV = u.BV // base BV: this campaign ignores pilot skills, so skill can never be a way to game the BV cap
			v.Card = catalog.CardFor(u)
		} else {
			v.Name, v.Unknown, v.Icon = hu.Unit, true, catalog.DefaultIconURL("")
		}
		c, note := mekbay.ConditionOf(hu, u.Tons, u.Engine) // 0 and "" for an unknown unit
		v.Condition, v.ConditionNote, v.AmmoExpended = string(c), note, hu.AmmoUsed > 0
		if known && u.Type == "Mek" {
			v.RepairLevels, v.RepairCosts, v.RearmCost = hangar.RepairOptions(hu, c, u.Tons), hangar.RepairCosts(u.Tons, u.TechBase), hangar.RearmCost(hu.AmmoBins)
			v.AmmoBreakdown = hangar.AmmoBreakdown(hu, u)
			v.SellFull = hangar.Pristine(hu)
			v.SellValue = hangar.SellValue(u.BV, v.SellFull)
			v.SwapOptions = hangar.SwapOptions(u)
		}
		views = append(views, v)
	}
	return views
}

// battleViews describes the units of a battle's force, for its live status: as hangarViews does, except that a pilot whose cockpit is gone is dead
// (mekbay.HangarUnit.PilotKilled), as MekBay's record sheet shows them, which pre-ticks Killed in action when the mission is completed. A hangar's crews are
// reset after every battle, so only here does a gone cockpit mean a dead pilot.
func battleViews(h mekbay.HangarForce) []hangarUnitView {
	views := hangarViews(h)
	for i, hu := range h.Units {
		views[i].PilotDead = hu.PilotKilled()
	}
	return views
}

// deployProblem checks a chosen set of hangar units against the hangar and the scenario's limits; it returns a
// message for the player, or "" when the choice is fine.
func deployProblem(h mekbay.HangarForce, ids []string, bvLimit int) string {
	if len(ids) == 0 {
		return "Choose at least one unit to deploy."
	}
	byID := map[string]hangarUnitView{}
	for _, v := range hangarViews(h) {
		byID[v.ID] = v
	}
	seen := map[string]bool{}
	total := 0
	for _, id := range ids {
		v, ok := byID[id]
		switch {
		case seen[id]:
			return "A unit was chosen twice."
		case !ok:
			return "A chosen unit is no longer in your hangar. Reopen the popup to reload it."
		case v.Destroyed:
			return v.Name + " is destroyed and can't be deployed."
		case v.Unknown:
			return v.Name + " isn't in the unit catalog, so its battle value is unknown."
		}
		seen[id] = true
		total += v.BV
	}
	if bvLimit > 0 && total > bvLimit {
		return fmt.Sprintf("The force is %d BV, over this scenario's limit of %d.", total, bvLimit)
	}
	return ""
}
