package site

import (
	"context"
	"strings"

	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/storage"
)

// Provided forces. A scenario can set the mechs its players fly instead of letting them bring their own, for story reasons (Scenario 000, the Trial of Position,
// hands every cadet one of four standard machines). Its Setup.md front matter lists them in `provided_units`, as MekBay unit IDs:
//
//	provided_units: [BMVulture_Prime, BMMadCat_Prime, BMBlackHawk_Prime, BMRyoken_Prime]
//
// Such a force is ephemeral. It exists only for that scenario: the Deploy popup lists these mechs in place of the hangar (each can be taken once, by one player),
// the rest of the deploy works as usual (named pilots, the scenario's BV limit, several players), and nothing about them is ever written to the hangar or cached as
// it. A provided mech is always new: no damage, no spent ammunition.

// providedPrefix starts the ID of a provided unit, which has no force-unit ID of its own because it is in no force.
const providedPrefix = "provided-"

// providedForce is the scenario's provided mechs as a hangar force, so everything that reads a hangar (the Deploy popup, the plan checks) works on it unchanged.
func providedForce(sc *content.Scenario) mekbay.HangarForce {
	h := mekbay.HangarForce{Name: "Provided mechs"}
	for _, name := range sc.ProvidedUnits {
		h.Units = append(h.Units, mekbay.HangarUnit{ID: providedPrefix + strings.ToLower(name), Unit: name, Group: "Provided", Gunnery: mekbay.DefaultGunnery, Piloting: mekbay.DefaultPiloting})
	}
	return h
}

// scenarioHangar is the force the scenario's Deploy works from: its own provided mechs, or else the player's hangar read from MekBay.
func (s *Server) scenarioHangar(ctx context.Context, run storage.CampaignRun, sc *content.Scenario) (mekbay.HangarForce, error) {
	if sc.Provides() {
		return providedForce(sc), nil
	}
	return s.mekbay.ReadHangar(ctx, run.HangarInstance)
}
