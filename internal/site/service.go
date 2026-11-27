package site

import (
	"fmt"
	"net/http"
	"time"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/format"
	"github.com/davidzech/Battletech/internal/hangar"
	"github.com/davidzech/Battletech/internal/mekbay"
)

// Repairing and rearming hangar units: the prices and which repairs apply are package hangar (repair.go), the edit to the force package mekbay.

// serviceUnit is POST /api/hangar/service: repair or rearm one unit of the hangar, paid for from the warchest. The server works out the unit's condition and the
// price itself; a repair names the level the page showed, and is refused if the unit has changed since.
func (s *Server) serviceUnit(w http.ResponseWriter, r *http.Request) {
	pt, c, _, ok := s.runRequest(w, r)
	if !ok {
		return
	}
	kind := r.PostFormValue("kind")
	if kind != "repair" && kind != "rearm" {
		http.Error(w, "Choose repair or rearm.", http.StatusBadRequest)
		return
	}
	unitID, level := r.PostFormValue("unit"), r.PostFormValue("level")
	warchest, sp, ok := s.changeHangar(w, r, pt, c, func(force map[string]any, h mekbay.HangarForce) (int, string, error) {
		hu, found := hangarUnit(h, unitID)
		if !found {
			return 0, "", hangarError{http.StatusNotFound, "That unit is no longer in your hangar."}
		}
		cat, known := catalog.ByName(hu.Unit)
		if !known || cat.Type != "Mek" {
			return 0, "", hangarError{http.StatusBadRequest, "Only mechs in the catalog can be serviced here."}
		}
		cond, _ := mekbay.ConditionOf(hu, cat.Tons, cat.Engine)
		cost := 0
		switch kind {
		case "repair":
			if cond == mekbay.CondTruly {
				return 0, "", hangarError{http.StatusConflict, "A truly destroyed unit can't be repaired."}
			}
			options := hangar.RepairOptions(hu, cond, cat.Tons)
			if len(options) == 0 {
				return 0, "", hangarError{http.StatusConflict, "That unit has nothing to repair."}
			}
			offered := false
			for _, o := range options {
				offered = offered || o == level
			}
			if !offered {
				return 0, "", hangarError{http.StatusConflict, "That repair doesn't apply to the unit's damage (it may have changed). Reload and choose again."}
			}
			cost = hangar.RepairCost(level, cat.Tons, cat.TechBase)
		case "rearm":
			if cond == mekbay.CondTruly {
				return 0, "", hangarError{http.StatusConflict, "A truly destroyed unit can only be sold."}
			}
			if hu.AmmoBins == 0 {
				return 0, "", hangarError{http.StatusConflict, "That unit has no spent ammunition."}
			}
			cost = hangar.RearmCost(hu.AmmoBins)
		}
		history := fmt.Sprintf("Rearmed %s (%d ammo bins) for %s SP.", catalog.DisplayName(cat), hu.AmmoBins, format.Commas(cost))
		if kind == "repair" {
			history = fmt.Sprintf("Repaired %s (%s) for %s SP.", catalog.DisplayName(cat), repairLevelText[level], format.Commas(cost))
		}
		crippled := cond == mekbay.CondCrippled || cond == mekbay.CondDestroyed // decides what the structure level restores
		if err := mekbay.ServiceUnitState(force, hu.ID, kind, level, cat.Tons, crippled, time.Now()); err != nil {
			return 0, "", refused(err)
		}
		return -cost, history, nil
	})
	if ok {
		writeCharge(w, -sp, warchest)
	}
}
