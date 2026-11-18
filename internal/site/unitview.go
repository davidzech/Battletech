package site

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
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
	PilotDead  bool    `json:"pilotDead"` // the record sheet shows the pilot dead
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
	AmmoBreakdown []ammoLine `json:"ammoBreakdown"`
	// SellValue is the SP the unit sells for (see sell.go), and SellFull whether that is the full price, which only a fully repaired and rearmed unit gets.
	SellValue int  `json:"sellValue"`
	SellFull  bool `json:"sellFull"`
	// SwapOptions are the other variants an omnimech can be reconfigured to, with their prices (see swap.go); empty for any other unit.
	SwapOptions []swapOption `json:"swapOptions,omitempty"`
}

var chassisNames = regexp.MustCompile(`^(.*?)\s*\((.+)\)$`)

// unitNames returns the name to show for a unit and its model. A design with two names is written "Inner Sphere
// name (Clan name)" in the catalog, and the Clan name always wins: "Vulture (Mad Dog)" is shown as "Mad Dog". Only
// Clan-tech designs follow that pattern; the few Inner Sphere ones with a parenthesis ("Katana (Crockett)") have no
// Clan name, so they keep their first name.
func unitNames(u Unit) (name, model string) {
	name = u.Chassis
	if m := chassisNames.FindStringSubmatch(u.Chassis); m != nil {
		if u.TechBase == "Clan" {
			name = m[2]
		} else {
			name = m[1]
		}
	}
	return name, u.Model
}

// roleLabel is the role shown in a mech row's band. The catalog gives a few mechs the role "None", which would read
// as a label in its own right, so those show their weight class instead (the first part of it, for the few classes
// that are long lists).
func roleLabel(u Unit) string {
	if r := strings.TrimSpace(u.Role); r != "" && !strings.EqualFold(r, "none") {
		return r
	}
	// A few classes are long lists ("Ultra Light/PA(L)/Exoskeleton"): the first part is the label.
	class, _, _ := strings.Cut(u.Class, "/")
	return strings.TrimSpace(class)
}

// displayName is a unit's name and model on one line, e.g. "Mad Dog Prime".
func displayName(u Unit) string {
	name, model := unitNames(u)
	return strings.TrimSpace(name + " " + model)
}

// deployedGroup is the hangar group whose units the Deploy wizard gives Player 1 to start with (a group the player may have made in MekBay).
const deployedGroup = "Deployed"

// hangarViews describes every unit of a hangar for the deploy popup.
func hangarViews(h HangarForce) []hangarUnitView {
	views := make([]hangarUnitView, 0, len(h.Units))
	for _, hu := range h.Units {
		v := hangarUnitView{MekBayURL: unitPageURL(hu.Unit), ID: hu.ID, Pilot: hu.Pilot, PilotDead: hu.PilotDead, Gunnery: hu.Gunnery, Piloting: hu.Piloting, Damaged: hu.Damaged, Destroyed: hu.Destroyed, InDeployed: sameGroupName(hu.Group, deployedGroup)}
		if u, ok := unitByName(hu.Unit); ok {
			v.Name, v.Model = unitNames(u)
			v.Class, v.Role, v.Tons, v.Icon = u.Class, roleLabel(u), u.Tons, iconURL(u)
			v.BV = u.BV // base BV: this campaign ignores pilot skills, so skill can never be a way to game the BV cap
		} else {
			v.Name, v.Unknown, v.Icon = hu.Unit, true, defaultIconURL("")
		}
		var tons float64
		var engine string
		if u, ok := unitByName(hu.Unit); ok {
			tons, engine = u.Tons, u.Engine
		}
		c, note := conditionOf(hu, tons, engine)
		v.Condition, v.ConditionNote, v.AmmoExpended = string(c), note, hu.AmmoUsed > 0
		if u, ok := unitByName(hu.Unit); ok && u.Type == "Mek" {
			v.RepairLevels, v.RepairCosts, v.RearmCost = repairOptions(hu, c, u.Tons), repairCosts(u.Tons, u.TechBase), rearmCost(hu.AmmoBins)
			v.AmmoBreakdown = ammoBreakdown(hu, u)
			v.SellFull = unitPristine(hu)
			v.SellValue = sellValue(u.BV, v.SellFull)
			v.SwapOptions = swapOptions(u)
		}
		views = append(views, v)
	}
	return views
}

// deployProblem checks a chosen set of hangar units against the hangar and the scenario's limits; it returns a
// message for the player, or "" when the choice is fine.
func deployProblem(h HangarForce, ids []string, bvLimit int) string {
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

// unitPageURL is the link to a unit's page on mekbay.com, from its MekBay unit ID (e.g. BMMadCat_H): the General tab of that design's page,
// not a unit in anyone's force. "" if there is no ID.
func unitPageURL(unitID string) string {
	if unitID == "" {
		return ""
	}
	return "https://mekbay.com/?" + url.Values{"gs": {"cbt"}, "shareUnit": {unitID}, "tab": {"General"}}.Encode()
}
