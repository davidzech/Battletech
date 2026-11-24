package hangar

import (
	"math"
	"sort"
	"strings"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/mekbay"
)

// Repairing and rearming a hangar unit between scenarios, from Hot Spots: Draconis Reach, the SP Activity Cost Table (printed p. 33).
//
//	Repair (armor damage only)                = tonnage / 2
//	Repair (structure and/or critical damage) = tonnage x 2
//	Repair (crippled)                         = tonnage x 3
//	Repair (destroyed)                        = tonnage x 5
//	Rearm, per ton of ammunition              = 10
//
// Only the highest repair level that applies is paid, and it covers every level beneath it. Clan and Mixed tech cost half again as much
// (x1.5, rounded up). A unit whose center torso structure is gone is truly destroyed and can't be repaired at all.

// repairLevels lists the levels with their cost multipliers on tonnage.
var repairLevels = []struct {
	ID   string
	Mult float64
}{{mekbay.RepairArmor, 0.5}, {mekbay.RepairStructure, 2}, {mekbay.RepairCrippled, 3}, {mekbay.RepairDestroyed, 5}}

// rearmCostPerTon is what a ton of expended ammunition costs to restore.
const rearmCostPerTon = 10

// RepairOptions lists the repairs that would actually fix something on a unit, lowest first. Any of them can be chosen on its own, so a crippled
// or destroyed unit may have only its armor repaired: armor if any armor is lost; structure if a surviving location has lost internal structure or
// has a critical hit (the engine's hits and destroyed locations belong to the crippled level); crippled for a crippled or destroyed unit; and
// destroyed for a destroyed one. A truly destroyed or undamaged unit has none.
func RepairOptions(hu mekbay.HangarUnit, c mekbay.Condition, tons float64) []string {
	if c == mekbay.CondTruly || c == mekbay.CondOK {
		return nil
	}
	var out []string
	for _, l := range hu.Locations {
		if l.Armor > 0 {
			out = append(out, mekbay.RepairArmor)
			break
		}
	}
	structure := false
	for k, l := range hu.Locations {
		if l.Internal > 0 && !mekbay.InternalGone(hu, k, tons) {
			structure = true
		}
	}
	for _, cr := range hu.Crits {
		if !cr.Engine && !mekbay.InternalGone(hu, cr.Loc, tons) {
			structure = true
		}
	}
	if structure {
		out = append(out, mekbay.RepairStructure)
	}
	if c == mekbay.CondCrippled || c == mekbay.CondDestroyed {
		out = append(out, mekbay.RepairCrippled)
	}
	if c == mekbay.CondDestroyed {
		out = append(out, mekbay.RepairDestroyed)
	}
	return out
}

// RepairCost is the SP a repair level costs for a unit of this tonnage and tech base.
func RepairCost(level string, tons float64, techBase string) int {
	for _, l := range repairLevels {
		if l.ID != level {
			continue
		}
		cost := tons * l.Mult
		if strings.EqualFold(techBase, "Clan") || strings.EqualFold(techBase, "Mixed") {
			cost *= 1.5
		}
		return int(math.Ceil(cost - 1e-9))
	}
	return 0
}

// RepairCosts lists every level's cost, for the repair dialog.
func RepairCosts(tons float64, techBase string) map[string]int {
	out := make(map[string]int, len(repairLevels))
	for _, l := range repairLevels {
		out[l.ID] = RepairCost(l.ID, tons, techBase)
	}
	return out
}

// RearmCost is the SP to refill a unit's spent ammunition. Each ammo bin that has been used counts as one ton (the usual bin size).
func RearmCost(bins int) int { return bins * rearmCostPerTon }

// AmmoLine is one ammo type of a rearm: how many tons are refilled and how many shots were used out of the shots those tons hold.
type AmmoLine struct {
	Name     string `json:"name"`
	Tons     int    `json:"tons"`
	Used     int    `json:"used"`
	Capacity int    `json:"capacity"`
}

// AmmoBreakdown lists what a rearm would refill, by ammo type, alphabetically. Each used bin is a ton; its capacity is the unit's shots for
// that ammo in that location divided by its slots there (the catalog knows that, MekBay only records the shots used). A bin the catalog doesn't
// list is still counted, with an unknown (zero) capacity.
func AmmoBreakdown(hu mekbay.HangarUnit, u catalog.Unit) []AmmoLine {
	var out []AmmoLine
	index := map[string]int{}
	for _, use := range hu.AmmoUsed2 {
		name, capacity := use.Name, 0
		for _, b := range u.Ammo {
			if b.ID == use.Name && b.Loc == use.Loc && b.Slots > 0 {
				name, capacity = b.Name, b.Shots/b.Slots
				break
			}
		}
		i, ok := index[name]
		if !ok {
			i = len(out)
			index[name] = i
			out = append(out, AmmoLine{Name: name})
		}
		out[i].Tons++
		out[i].Used += use.Used
		out[i].Capacity += capacity
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out
}
