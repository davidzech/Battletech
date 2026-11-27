package site

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/format"
	"github.com/davidzech/Battletech/internal/hangar"
	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/storage"
)

// Reconfiguring omnimechs: the price and the variants offered are package hangar (swap.go), the edit to the force package mekbay.

// markSwapAvailability marks each unit's reconfigure options as available or not to the campaign's faction in its era.
func markSwapAvailability(views []hangarUnitView, c *content.Campaign) {
	var available map[string]bool
	if c != nil && c.AvailabilityFaction != "" && c.AvailabilityEra != "" {
		available = catalog.Available(c.AvailabilityFaction, c.AvailabilityEra)
	}
	for i := range views {
		for j := range views[i].SwapOptions {
			o := &views[i].SwapOptions[j]
			o.Available = available == nil || available[o.Unit]
		}
	}
}

// swapUnit is POST /api/hangar/swap: reconfigure the hangar unit named by `unit` to the variant `to`. The server works the price out itself, refuses a unit that isn't
// fully repaired and rearmed or a variant that isn't one of its chassis's, and charges the warchest in the same transaction that makes the change.
func (s *Server) swapUnit(w http.ResponseWriter, r *http.Request) {
	pt, c, _, ok := s.runRequest(w, r)
	if !ok {
		return
	}
	unitID, to := r.PostFormValue("unit"), r.PostFormValue("to")
	warchest, sp, ok := s.changeHangar(w, r, pt, c, func(force map[string]any, h mekbay.HangarForce) (int, string, error) {
		hu, found := hangarUnit(h, unitID)
		if !found {
			return 0, "", hangarError{http.StatusNotFound, "That unit is no longer in your hangar."}
		}
		cat, known := catalog.ByName(hu.Unit)
		if !known || cat.Type != "Mek" || cat.Omni == 0 {
			return 0, "", hangarError{http.StatusBadRequest, "Only omnimechs can be reconfigured."}
		}
		if !hangar.Pristine(hu) {
			return 0, "", hangarError{http.StatusConflict, "An omnimech has to be fully repaired and rearmed before it can be reconfigured."}
		}
		var option *hangar.SwapOption
		for _, o := range hangar.SwapOptions(cat) {
			if o.Unit == to {
				o := o
				option = &o
			}
		}
		if option == nil {
			return 0, "", hangarError{http.StatusBadRequest, "That isn't a variant this omnimech can be reconfigured to."}
		}
		if err := mekbay.SwapUnitState(force, hu.ID, to, time.Now()); err != nil {
			return 0, "", refused(err)
		}
		return -option.Cost, fmt.Sprintf("Reconfigured %s to %s for %s SP.", catalog.DisplayName(cat), option.Model, format.Commas(option.Cost)), nil
	})
	if ok {
		writeCharge(w, -sp, warchest)
	}
}

// plannedSwap is a hangar omnimech the Deploy wizard reconfigures before its battle: proposed on the review, paid for and written to the hangar by Start battle.
type plannedSwap struct {
	unitID string
	name   string // the mech's display name, for the history line
	to     string // the MekBay unit ID of the variant
	model  string // the variant's model, for the history line
	cost   int    // SP
}

// reconfigureHangar applies the variants a deploy plan proposes for hangar omnimechs (the paired, repeated variant_unit and variant_to fields, as for provided
// mechs) and returns the force as the players will fly it, the swaps to pay for, and the problem as a sentence. The rules are the Manage page's Reconfigure:
// an omnimech fully repaired and rearmed, and another variant of its chassis, at the same price. A variant that is the unit's own changes
// nothing. The unit keeps its ID, so the plan's ticks and pilot assignments still point at it.
func reconfigureHangar(h mekbay.HangarForce, ids, tos []string) (mekbay.HangarForce, []plannedSwap, string) {
	if len(ids) == 0 && len(tos) == 0 {
		return h, nil, ""
	}
	if len(ids) != len(tos) {
		return h, nil, "Each reconfigured mech needs a variant."
	}
	out := h
	out.Units = append([]mekbay.HangarUnit(nil), h.Units...)
	done := map[string]bool{}
	var swaps []plannedSwap
	for k, id := range ids {
		if done[id] {
			return h, nil, "A mech was reconfigured twice."
		}
		done[id] = true
		i := -1
		for j := range out.Units {
			if out.Units[j].ID == id {
				i = j
			}
		}
		if i < 0 {
			return h, nil, "A reconfigured mech is no longer in your hangar. Reopen the popup to reload it."
		}
		hu := out.Units[i]
		to := tos[k]
		if to == hu.Unit {
			continue
		}
		cat, known := catalog.ByName(hu.Unit)
		if !known || cat.Type != "Mek" || cat.Omni == 0 {
			return h, nil, "Only omnimechs can be reconfigured."
		}
		name := catalog.DisplayName(cat)
		if !hangar.Pristine(hu) {
			return h, nil, name + " has to be fully repaired and rearmed before it can be reconfigured."
		}
		var option *hangar.SwapOption
		for _, o := range hangar.SwapOptions(cat) {
			if o.Unit == to {
				o := o
				option = &o
			}
		}
		if option == nil {
			return h, nil, fmt.Sprintf("%s can't be reconfigured to that variant.", name)
		}
		out.Units[i].Unit = to
		swaps = append(swaps, plannedSwap{unitID: id, name: name, to: to, model: option.Model, cost: option.Cost})
	}
	return out, swaps, ""
}

// swapsCost is what a plan's reconfigurations cost together.
func swapsCost(swaps []plannedSwap) int {
	total := 0
	for _, sw := range swaps {
		total += sw.cost
	}
	return total
}

// reconfigureForBattle pays for a plan's reconfigurations and makes them in the hangar, all in one transaction, before Start battle builds the battle from it: the
// plan is checked again against the hangar as it is then, and if any of it can't be made (or the warchest can't cover it) none of it is. Each is logged like a
// Reconfigure on the Manage page. Made, they stay made if the battle then fails to build: they are paid for and in the History, and Start battle can just be
// tried again. It returns the problem and its status, or "".
func (s *Server) reconfigureForBattle(ctx context.Context, r *http.Request, bp *builtPlan) (string, int) {
	ids, tos := make([]string, len(bp.swaps)), make([]string, len(bp.swaps))
	for i, sw := range bp.swaps {
		ids[i], tos[i] = sw.unitID, sw.to
	}
	var made []plannedSwap
	cost := 0
	// The start holds its claim for this scenario while it reconfigures (Store.ClaimBattle), which the guard allows; any other battle refuses it.
	left, err := s.store.EditHangarGuarded(ctx, bp.pt.ID, bp.c.Slug, hangarGuard(bp.c, bp.sc.ID), func(force map[string]any) (int, error) {
		h, err := parseHangarMap(force)
		if err != nil {
			return 0, err
		}
		_, swaps, msg := reconfigureHangar(h, ids, tos)
		if msg != "" {
			return 0, hangarError{http.StatusConflict, msg}
		}
		now := time.Now()
		for _, sw := range swaps {
			if err := mekbay.SwapUnitState(force, sw.unitID, sw.to, now); err != nil {
				return 0, refused(err)
			}
		}
		made, cost = swaps, swapsCost(swaps)
		return -cost, nil
	})
	var refusal hangarError
	var battle storage.BattleInProgressError
	switch {
	case errors.As(err, &refusal):
		return refusal.msg, refusal.status
	case errors.As(err, &battle):
		return battleRefusal(bp.c, battle), http.StatusConflict
	case errors.Is(err, storage.ErrInsufficientFunds):
		return fmt.Sprintf("Not enough in the warchest: reconfiguring costs %d SP and you have %d.", cost, left), http.StatusConflict
	case errors.Is(err, storage.ErrNoHangar):
		return noHangarMessage, http.StatusConflict
	case err != nil:
		log.Printf("reconfigure for the battle: %v", err)
		return "could not reconfigure your mechs", http.StatusInternalServerError
	}
	for _, sw := range made {
		s.logHistory(r, bp.pt.ID, bp.c.Slug, storage.HistoryHangar, -sw.cost, fmt.Sprintf("Reconfigured %s to %s for %s SP before %s.", sw.name, sw.model, format.Commas(sw.cost), bp.sc.Title))
	}
	return "", 0
}
