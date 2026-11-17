package site

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"
)

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
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := dialMekBay(ctx, c.url(), req.OwnerUID)
	if err != nil {
		return err
	}
	defer s.close()
	raw, err := s.call(ctx, map[string]any{"action": "getForce", "uuid": req.OwnerUID, "instanceId": req.Instance, "ownedOnly": false})
	if err != nil {
		return err
	}
	var reply struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil || reply.Data == nil {
		return errors.New("MekBay has no force for your hangar link; check that the link is saved in MekBay")
	}
	force := reply.Data
	now := time.Now()
	addUnitsToForce(force, req.Units, now)
	delete(force, "owned") // set by the server for readers; the app never sends it back
	force["timestamp"] = mekbayTime(now)
	_, err = s.call(ctx, map[string]any{"action": "saveForce", "uuid": req.OwnerUID, "data": force, "savedForceCount": 1})
	return err
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
		u, ok := unitByName(id)
		if !ok || u.Type != "Mek" {
			return 0, fmt.Errorf("%q is not a mech in the unit list.", id)
		}
		total += roundUp10(u.BV)
	}
	return total, nil
}

// purchaseUnits is POST /api/hangar/purchase: buy the units named by repeated `unit` fields, paid from the warchest at their base BV, and add
// them to the hangar in MekBay. It needs modify mode. The warchest is charged first and refunded if MekBay fails.
func (s *Server) purchaseUnits(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	run, started, err := s.store.Run(r.Context(), pt.ID, c.Slug)
	if err != nil || !started {
		http.Error(w, "Start this campaign first.", http.StatusBadRequest)
		return
	}
	if !run.Owned() {
		http.Error(w, notOwnedMessage, http.StatusConflict)
		return
	}
	ids := r.PostForm["unit"]
	cost, err := purchaseCost(ids)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.store.SpendWarchest(r.Context(), pt.ID, c.Slug, cost); errors.Is(err, ErrInsufficientFunds) {
		http.Error(w, fmt.Sprintf("Not enough in the warchest: that costs %d SP and you have %d.", cost, run.Warchest), http.StatusConflict)
		return
	} else if err != nil {
		http.Error(w, "could not charge the warchest", http.StatusInternalServerError)
		return
	}
	if err := s.mekbay.PurchaseUnits(r.Context(), PurchaseRequest{OwnerUID: accountUID(currentSeed(r), SlotPlayer1), Instance: run.HangarInstance, Units: ids}); err != nil {
		log.Printf("buy units in MekBay: %v", err) // never log the owner UID
		if rerr := s.store.RefundWarchest(r.Context(), pt.ID, c.Slug, cost); rerr != nil {
			log.Printf("could not refund %d SP: %v", cost, rerr)
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"cost":%d,"warchest":%d}`, cost, run.Warchest-cost)
}
