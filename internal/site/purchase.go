package site

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/davidzech/Battletech/internal/catalog"
)

// hangarGroup is the name of the group a hangar force's units sit in, where a purchase adds new units.
const hangarGroup = "Hangar"

// sameGroupName compares group names the way the site reads them: ignoring case and surrounding space.
func sameGroupName(name, want string) bool {
	return strings.EqualFold(strings.TrimSpace(name), want)
}

// Buying new units for the hangar. The price is the unit's base BV in SP (this campaign counts BV as SP, Hot Spots: Draconis Reach, SP Activity
// Cost Table: Purchase Unit = Battle Value). A purchase adds each unit, undamaged and with a standard 4/5 pilot, to the "Hangar" group of the
// player's force in MekBay, and is paid from the warchest.

// maxPurchase is how many units one purchase may add, so a mistake can't empty the warchest into dozens of units at once.
const maxPurchase = 10

// PurchaseRequest asks MekBay to add units to the player's hangar force.
type PurchaseRequest struct {
	OwnerUID string
	Instance string
	Units    []string // MekBay unit IDs (e.g. BMMadCat_H), one entry per unit bought
}

// addUnitsToForce adds a fresh, undamaged unit for each ID to the "Hangar" group of a saved force (the decoded JSON of its `data`), creating the
// group if it doesn't exist. Everything else in the force is left as it was.
func addUnitsToForce(force map[string]any, unitIDs []string, now time.Time) {
	groups, _ := force["groups"].([]any)
	var hangar map[string]any
	for _, g := range groups {
		if gm, ok := g.(map[string]any); ok {
			if name, _ := gm["name"].(string); hangar == nil && sameGroupName(name, hangarGroup) {
				hangar = gm
			}
		}
	}
	if hangar == nil {
		hangar = map[string]any{"id": newID(), "name": hangarGroup, "color": "", "units": []any{}}
		groups = append(groups, hangar)
		force["groups"] = groups
	}
	units, _ := hangar["units"].([]any)
	for _, id := range unitIDs {
		units = append(units, map[string]any{
			"id": newID(), "unit": id, "alias": "", "updatedTs": now.UnixMilli(),
			"state": map[string]any{
				"crew":      []any{map[string]any{"id": 0, "name": "", "gunnerySkill": 4, "pilotingSkill": 5, "hits": 0, "state": 0}},
				"crits":     []any{},
				"heat":      map[string]any{"current": 0, "previous": 0},
				"locations": map[string]any{},
				"modified":  false,
				"destroyed": false,
				"turnState": map[string]any{"applyMovePSR": false},
			},
		})
	}
	hangar["units"] = units
}

// PurchaseUnits adds units to the player's hangar force. It needs the owner's UID, because MekBay only accepts the owner's saves.
func (c *MekBayClient) PurchaseUnits(ctx context.Context, req PurchaseRequest) error {
	return c.editForce(ctx, req.OwnerUID, req.Instance, func(force map[string]any, now time.Time) error {
		addUnitsToForce(force, req.Units, now)
		return nil
	})
}

// purchaseCost is the SP price of a list of units, or an error naming the first one that can't be bought (not in the catalog, or not a mech).
func purchaseCost(ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, errors.New("Choose at least one unit to buy.")
	}
	if len(ids) > maxPurchase {
		return 0, fmt.Errorf("You can buy at most %d units at a time.", maxPurchase)
	}
	total := 0
	for _, id := range ids {
		u, ok := catalog.ByName(id)
		if !ok || u.Type != "Mek" {
			return 0, fmt.Errorf("%q is not a mech in the unit list.", id)
		}
		total += u.BV
	}
	return total, nil
}

// purchaseUnits is POST /api/hangar/purchase: buy the units named by repeated `unit` fields, paid from the warchest at their base BV, and add
// them to the hangar in MekBay. It needs modify mode. The warchest is charged first and refunded if MekBay fails.
func (s *Server) purchaseUnits(w http.ResponseWriter, r *http.Request) {
	pt, c, run, ok := s.hangarRequest(w, r)
	if !ok {
		return
	}
	ids := r.PostForm["unit"]
	cost, err := purchaseCost(ids)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.payForHangarChange(w, r, pt, c, run, cost, "buy units", func() error {
		return s.mekbay.PurchaseUnits(r.Context(), PurchaseRequest{OwnerUID: accountUID(currentSeed(r), SlotHangar), Instance: run.HangarInstance, Units: ids})
	})
}
