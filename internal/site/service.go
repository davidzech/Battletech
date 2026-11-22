package site

import (
	"log"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/davidzech/Battletech/internal/mekbay"

	"github.com/davidzech/Battletech/internal/catalog"
)

// Repairing and rearming a hangar unit between sorties, from Hot Spots: Draconis Reach, the SP Activity Cost Table (printed p. 33).
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

// repairOptions lists the repairs that would actually fix something on a unit, lowest first. Any of them can be chosen on its own, so a crippled
// or destroyed unit may have only its armor repaired: armor if any armor is lost; structure if a surviving location has lost internal structure or
// has a critical hit (the engine's hits and destroyed locations belong to the crippled level); crippled for a crippled or destroyed unit; and
// destroyed for a destroyed one. A truly destroyed or undamaged unit has none.
func repairOptions(hu mekbay.HangarUnit, c mekbay.Condition, tons float64) []string {
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

// repairCost is the SP a repair level costs for a unit of this tonnage and tech base.
func repairCost(level string, tons float64, techBase string) int {
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

// repairCosts lists every level's cost, for the repair dialog.
func repairCosts(tons float64, techBase string) map[string]int {
	out := make(map[string]int, len(repairLevels))
	for _, l := range repairLevels {
		out[l.ID] = repairCost(l.ID, tons, techBase)
	}
	return out
}

// rearmCost is the SP to refill a unit's spent ammunition. Each ammo bin that has been used counts as one ton (the usual bin size).
func rearmCost(bins int) int { return bins * rearmCostPerTon }

// ammoLine is one ammo type of a rearm: how many tons are refilled and how many shots were used out of the shots those tons hold.
type ammoLine struct {
	Name     string `json:"name"`
	Tons     int    `json:"tons"`
	Used     int    `json:"used"`
	Capacity int    `json:"capacity"`
}

// ammoBreakdown lists what a rearm would refill, by ammo type, alphabetically. Each used bin is a ton; its capacity is the unit's shots for
// that ammo in that location divided by its slots there (the catalog knows that, MekBay only records the shots used). A bin the catalog doesn't
// list is still counted, with an unknown (zero) capacity.
func ammoBreakdown(hu mekbay.HangarUnit, u catalog.Unit) []ammoLine {
	var out []ammoLine
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
			out = append(out, ammoLine{Name: name})
		}
		out[i].Tons++
		out[i].Used += use.Used
		out[i].Capacity += capacity
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out
}

// serviceUnit is POST /api/hangar/service: repair or rearm one unit of the hangar, paid for from the warchest. It needs modify mode, since
// the change is made in MekBay. The server works out the unit's condition and the price itself; a repair names the level the page showed, and
// is refused if the unit has changed since.
func (s *Server) serviceUnit(w http.ResponseWriter, r *http.Request) {
	pt, c, run, ok := s.hangarRequest(w, r)
	if !ok {
		return
	}
	kind := r.PostFormValue("kind")
	if kind != "repair" && kind != "rearm" {
		http.Error(w, "Choose repair or rearm.", http.StatusBadRequest)
		return
	}
	h, err := s.mekbay.ReadHangar(r.Context(), run.HangarInstance)
	if err != nil {
		log.Printf("read hangar to %s a unit: %v", kind, err)
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
		http.Error(w, "Only mechs in the catalog can be serviced here.", http.StatusBadRequest)
		return
	}
	cost, level := 0, ""
	switch kind {
	case "repair":
		cond, _ := mekbay.ConditionOf(hu, cat.Tons, cat.Engine)
		if cond == mekbay.CondTruly {
			http.Error(w, "A truly destroyed unit can't be repaired.", http.StatusConflict)
			return
		}
		options := repairOptions(hu, cond, cat.Tons)
		if len(options) == 0 {
			http.Error(w, "That unit has nothing to repair.", http.StatusConflict)
			return
		}
		level = r.PostFormValue("level")
		offered := false
		for _, o := range options {
			offered = offered || o == level
		}
		if !offered {
			http.Error(w, "That repair doesn't apply to the unit's damage (it may have changed). Reload and choose again.", http.StatusConflict)
			return
		}
		cost = repairCost(level, cat.Tons, cat.TechBase)
	case "rearm":
		if cond, _ := mekbay.ConditionOf(hu, cat.Tons, cat.Engine); cond == mekbay.CondTruly {
			http.Error(w, "A truly destroyed unit can only be sold.", http.StatusConflict)
			return
		}
		if hu.AmmoBins == 0 {
			http.Error(w, "That unit has no spent ammunition.", http.StatusConflict)
			return
		}
		cost = rearmCost(hu.AmmoBins)
	}
	s.payForHangarChange(w, r, pt, c, run, cost, kind+" a unit", func() error {
		return s.mekbay.ServiceUnit(r.Context(), mekbay.ServiceRequest{OwnerUID: mekbay.AccountUID(currentSeed(r), mekbay.SlotHangar), Instance: run.HangarInstance, UnitID: hu.ID, Kind: kind, Level: level, Tons: cat.Tons})
	})
}
