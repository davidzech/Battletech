package site

import (
	"context"
	"fmt"
	"strings"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/hangar"
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

// reconfigureProvided applies the variants a deploy plan chose for the provided omnimechs (the paired, repeated variant_unit and variant_to fields: a provided
// unit's ID and the MekBay unit ID of the variant it becomes) and returns the force as the players will fly it, or the problem as a sentence. Reconfiguring is free
// and only offered when the scenario says so (provided_swaps): to another variant of the same chassis that the campaign's faction fields in its era. A variant
// that is the unit's own changes nothing. The unit keeps its ID, so the plan's ticks and pilot assignments still point at it.
func reconfigureProvided(h mekbay.HangarForce, c *content.Campaign, sc *content.Scenario, ids, tos []string) (mekbay.HangarForce, string) {
	if len(ids) == 0 && len(tos) == 0 {
		return h, ""
	}
	if !sc.Provides() || !sc.ProvidedSwaps {
		return h, "This mission's mechs can't be reconfigured."
	}
	if len(ids) != len(tos) {
		return h, "Each reconfigured mech needs a variant."
	}
	var available map[string]bool
	if c != nil && c.AvailabilityFaction != "" && c.AvailabilityEra != "" {
		available = catalog.Available(c.AvailabilityFaction, c.AvailabilityEra)
	}
	out := h
	out.Units = append([]mekbay.HangarUnit(nil), h.Units...)
	done := map[string]bool{}
	for k, id := range ids {
		if done[id] {
			return h, "A mech was reconfigured twice."
		}
		done[id] = true
		i := -1
		for j := range out.Units {
			if out.Units[j].ID == id {
				i = j
			}
		}
		if i < 0 {
			return h, "That mech isn't one of this mission's."
		}
		to := tos[k]
		if to == out.Units[i].Unit {
			continue
		}
		cat, known := catalog.ByName(out.Units[i].Unit)
		ok := false
		if known {
			for _, o := range hangar.SwapOptions(cat) {
				if o.Unit == to && (available == nil || available[to]) {
					ok = true
				}
			}
		}
		if !ok {
			name := id
			if known {
				name = catalog.DisplayName(cat)
			}
			return h, fmt.Sprintf("%s can't be reconfigured to that variant.", name)
		}
		out.Units[i].Unit = to
	}
	return out, ""
}
