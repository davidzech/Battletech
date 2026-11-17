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

// Selling units. A unit sells for half its base BV in SP (this campaign counts BV as SP, as purchasing does), but only a unit that is fully repaired and
// rearmed gets that: any damage or spent ammunition halves the price again, rounded down, so a worn unit fetches about a quarter of its BV. A truly destroyed
// mech (see CondTruly) can't be repaired, so selling it is the only thing left to do with it, and it always sells at the reduced price. The sale takes the unit
// out of the hangar force (crew and all) and pays the warchest.

// unitPristine reports whether a unit is fully repaired and rearmed: no recorded damage, not destroyed, no spent ammunition.
func unitPristine(hu HangarUnit) bool {
	return !hu.Damaged && !hu.Destroyed && hu.AmmoUsed == 0 && hu.AmmoBins == 0 && len(hu.Locations) == 0 && len(hu.Crits) == 0
}

// sellValue is the SP a unit with this base BV sells for: half its BV when pristine, otherwise half of that, rounding down each time.
func sellValue(bv int, pristine bool) int {
	v := bv / 2
	if !pristine {
		v /= 2
	}
	return v
}

// removeUnitFromForce takes the unit with this ID out of a saved force (the decoded JSON of its `data`); false if it isn't there.
func removeUnitFromForce(force map[string]any, unitID string) bool {
	groups, _ := force["groups"].([]any)
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		units, _ := gm["units"].([]any)
		for i, u := range units {
			if um, _ := u.(map[string]any); um != nil && um["id"] == unitID {
				gm["units"] = append(units[:i:i], units[i+1:]...)
				return true
			}
		}
	}
	return false
}

// SellRequest asks MekBay to remove a unit from the site's copy of the hangar.
type SellRequest struct {
	OwnerUID string
	Instance string
	UnitID   string // the unit's own ID inside the force
}

// SellUnit removes a unit from the hangar force. It needs the owner's UID, because MekBay only accepts the owner's saves.
func (c *MekBayClient) SellUnit(ctx context.Context, req SellRequest) error {
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
		return errors.New("MekBay has no force for your hangar; try importing it again")
	}
	force := reply.Data
	if !removeUnitFromForce(force, req.UnitID) {
		return errors.New("that unit is no longer in your hangar force")
	}
	delete(force, "owned") // set by the server for readers; the app never sends it back
	force["timestamp"] = mekbayTime(time.Now())
	_, err = s.call(ctx, map[string]any{"action": "saveForce", "uuid": req.OwnerUID, "data": force, "savedForceCount": 1})
	return err
}

// sellUnit is POST /api/hangar/sell: sell the hangar unit named by `unit`. The server works out the price itself from the unit's state. The unit is taken out of
// the hangar first and the warchest paid after, so a failure in MekBay never pays for a unit the player still has.
func (s *Server) sellUnit(w http.ResponseWriter, r *http.Request) {
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
	h, err := s.mekbay.ReadHangar(r.Context(), run.HangarInstance)
	if err != nil {
		log.Printf("read hangar to sell a unit: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	var hu HangarUnit
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
	cat, known := unitByName(hu.Unit)
	if !known || cat.Type != "Mek" {
		http.Error(w, "Only mechs in the catalog can be sold here.", http.StatusBadRequest)
		return
	}
	value := sellValue(cat.BV, unitPristine(hu))
	if err := s.mekbay.SellUnit(r.Context(), SellRequest{OwnerUID: accountUID(currentSeed(r), SlotPlayer1), Instance: run.HangarInstance, UnitID: hu.ID}); err != nil {
		log.Printf("sell a unit in MekBay: %v", err) // never log the owner UID
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if err := s.store.RefundWarchest(r.Context(), pt.ID, c.Slug, value); err != nil {
		log.Printf("could not pay %d SP for a sold unit: %v", value, err)
		http.Error(w, "The unit was sold but the warchest could not be paid. Set the funds under Advanced Management.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"value":%d,"warchest":%d}`, value, run.Warchest+value)
}
