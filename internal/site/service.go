package site

import (
	"fmt"
	"log"
	"net/http"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/format"
	"github.com/davidzech/Battletech/internal/hangar"
	"github.com/davidzech/Battletech/internal/mekbay"
)

// Repairing and rearming hangar units: the prices and which repairs apply are package hangar (repair.go), the edit to the force package mekbay.

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
	hu, found := hangarUnit(h, r.PostFormValue("unit"))
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
		options := hangar.RepairOptions(hu, cond, cat.Tons)
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
		cost = hangar.RepairCost(level, cat.Tons, cat.TechBase)
	case "rearm":
		if cond, _ := mekbay.ConditionOf(hu, cat.Tons, cat.Engine); cond == mekbay.CondTruly {
			http.Error(w, "A truly destroyed unit can only be sold.", http.StatusConflict)
			return
		}
		if hu.AmmoBins == 0 {
			http.Error(w, "That unit has no spent ammunition.", http.StatusConflict)
			return
		}
		cost = hangar.RearmCost(hu.AmmoBins)
	}
	history := fmt.Sprintf("Rearmed %s (%d ammo bins) for %s SP.", catalog.DisplayName(cat), hu.AmmoBins, format.Commas(cost))
	if kind == "repair" {
		history = fmt.Sprintf("Repaired %s (%s) for %s SP.", catalog.DisplayName(cat), repairLevelText[level], format.Commas(cost))
	}
	cond, _ := mekbay.ConditionOf(hu, cat.Tons, cat.Engine)
	crippled := cond == mekbay.CondCrippled || cond == mekbay.CondDestroyed // decides what the structure level restores
	s.payForHangarChange(w, r, pt, c, run, cost, kind+" a unit", history, func() (mekbay.HangarForce, error) {
		return s.mekbay.ServiceUnit(r.Context(), mekbay.ServiceRequest{OwnerUID: mekbay.AccountUID(currentSeed(r), mekbay.SlotHangar), Instance: run.HangarInstance, UnitID: hu.ID, Kind: kind, Level: level, Tons: cat.Tons, Crippled: crippled})
	})
}
