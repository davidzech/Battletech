package site

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
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

// Repair levels, lowest first.
const (
	RepairArmor     = "armor"
	RepairStructure = "structure"
	RepairCrippled  = "crippled"
	RepairDestroyed = "destroyed"
)

// repairLevels lists the levels with their cost multipliers on tonnage.
var repairLevels = []struct {
	ID   string
	Mult float64
}{{RepairArmor, 0.5}, {RepairStructure, 2}, {RepairCrippled, 3}, {RepairDestroyed, 5}}

// rearmCostPerTon is what a ton of expended ammunition costs to restore.
const rearmCostPerTon = 10

// repairOptions lists the repairs that would actually fix something on a unit, lowest first. Any of them can be chosen on its own, so a crippled
// or destroyed unit may have only its armor repaired: armor if any armor is lost; structure if a surviving location has lost internal structure or
// has a critical hit (the engine's hits and destroyed locations belong to the crippled level); crippled for a crippled or destroyed unit; and
// destroyed for a destroyed one. A truly destroyed or undamaged unit has none.
func repairOptions(hu HangarUnit, c Condition, tons float64) []string {
	if c == CondTruly || c == CondOK {
		return nil
	}
	var out []string
	for _, l := range hu.Locations {
		if l.Armor > 0 {
			out = append(out, RepairArmor)
			break
		}
	}
	structure := false
	for k, l := range hu.Locations {
		if l.Internal > 0 && !internalGone(hu, k, tons) {
			structure = true
		}
	}
	for _, cr := range hu.Crits {
		if !cr.Engine && !internalGone(hu, cr.Loc, tons) {
			structure = true
		}
	}
	if structure {
		out = append(out, RepairStructure)
	}
	if c == CondCrippled || c == CondDestroyed {
		out = append(out, RepairCrippled)
	}
	if c == CondDestroyed {
		out = append(out, RepairDestroyed)
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
func ammoBreakdown(hu HangarUnit, u Unit) []ammoLine {
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

// ServiceRequest asks MekBay to clear a unit's damage (repair) or refill its ammunition (rearm) in the player's hangar force.
type ServiceRequest struct {
	OwnerUID string
	Instance string
	UnitID   string
	Kind     string  // "repair" or "rearm"
	Level    string  // for a repair, the level (armor, structure, crippled, destroyed)
	Tons     float64 // the unit's tonnage, to tell which locations are destroyed
}

// serviceUnitState applies a repair or rearm to the unit with this ID inside a force in MekBay's save format. Each repair level fixes its own
// damage and the levels beneath it, and nothing above:
//
//	armor      the lost armor, everywhere
//	structure  also internal structure in locations that are not destroyed, and critical hits there (but not the engine's, which cripple)
//	crippled   also destroyed locations and the engine's hits: all damage, so every location and every critical slot is cleared
//	destroyed  also the destroyed flag
//
// A rearm refills the ammunition and leaves the damage.
func serviceUnitState(force map[string]any, unitID, kind, level string, tons float64) error {
	groups, _ := force["groups"].([]any)
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		units, _ := gm["units"].([]any)
		for _, u := range units {
			um, _ := u.(map[string]any)
			if um == nil || um["id"] != unitID {
				continue
			}
			state, _ := um["state"].(map[string]any)
			if state == nil {
				return errors.New("that unit has no recorded state to change")
			}
			crits, _ := state["crits"].([]any)
			switch kind {
			case "repair":
				if err := repairState(state, crits, level, tons); err != nil {
					return err
				}
			case "rearm":
				for _, c := range crits {
					if cm, ok := c.(map[string]any); ok {
						if _, has := cm["consumed"]; has {
							cm["consumed"] = 0
						}
					}
				}
			default:
				return fmt.Errorf("unknown service %q", kind)
			}
			um["updatedTs"] = time.Now().UnixMilli()
			return nil
		}
	}
	return errors.New("that unit is no longer in your hangar force")
}

// clearCrit takes a critical slot's damage away and keeps its spent ammunition.
func clearCrit(cm map[string]any) {
	delete(cm, "destroyed")
	delete(cm, "destroying")
	delete(cm, "destroyedTurn")
	if _, has := cm["hits"]; has {
		cm["hits"] = 0
	}
}

func repairState(state map[string]any, crits []any, level string, tons float64) error {
	locs, _ := state["locations"].(map[string]any)
	loss := func(m map[string]any, key string) int { f, _ := m[key].(float64); return int(f) }
	gone := func(loc string, m map[string]any) bool {
		max := maxStructure(loc, tons)
		return max > 0 && loss(m, "internal") >= max
	}
	switch level {
	case RepairArmor, RepairStructure:
		for k, v := range locs {
			m, _ := v.(map[string]any)
			if m == nil {
				continue
			}
			m["armor"] = 0
			if level == RepairStructure && !gone(k, m) {
				m["internal"] = 0
			}
			if loss(m, "internal") == 0 {
				delete(locs, k)
			}
		}
		if level == RepairStructure {
			for _, c := range crits {
				cm, ok := c.(map[string]any)
				if !ok {
					continue
				}
				loc, _ := cm["loc"].(string)
				name, _ := cm["name"].(string)
				if lm, _ := locs[loc].(map[string]any); lm != nil && gone(loc, lm) {
					continue // a destroyed location's slots go with the location
				}
				if strings.Contains(strings.ToLower(name), "engine") {
					continue // engine hits are what cripples a unit
				}
				clearCrit(cm)
			}
		}
	case RepairCrippled, RepairDestroyed:
		state["locations"] = map[string]any{}
		delete(state, "ruleChecks")
		for _, c := range crits {
			if cm, ok := c.(map[string]any); ok {
				clearCrit(cm)
			}
		}
		if level == RepairDestroyed {
			state["destroyed"] = false
		}
	default:
		return fmt.Errorf("unknown repair level %q", level)
	}
	return nil
}

// ServiceUnit repairs or rearms a unit in the player's hangar force. It needs the owner's UID, because MekBay only accepts the owner's saves.
func (c *MekBayClient) ServiceUnit(ctx context.Context, req ServiceRequest) error {
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
	if err := serviceUnitState(force, req.UnitID, req.Kind, req.Level, req.Tons); err != nil {
		return err
	}
	delete(force, "owned") // set by the server for readers; the app never sends it back
	force["timestamp"] = mekbayTime(time.Now())
	_, err = s.call(ctx, map[string]any{"action": "saveForce", "uuid": req.OwnerUID, "data": force, "savedForceCount": 1})
	return err
}

// serviceUnit is POST /api/hangar/service: repair or rearm one unit of the hangar, paid for from the warchest. It needs modify mode, since
// the change is made in MekBay. The server works out the unit's condition and the price itself; a repair names the level the page showed, and
// is refused if the unit has changed since.
func (s *Server) serviceUnit(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, "Only mechs in the catalog can be serviced here.", http.StatusBadRequest)
		return
	}
	cost, level := 0, ""
	switch kind {
	case "repair":
		cond, _ := conditionOf(hu, cat.Tons, cat.Engine)
		if cond == CondTruly {
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
		if cond, _ := conditionOf(hu, cat.Tons, cat.Engine); cond == CondTruly {
			http.Error(w, "A truly destroyed unit can only be sold.", http.StatusConflict)
			return
		}
		if hu.AmmoBins == 0 {
			http.Error(w, "That unit has no spent ammunition.", http.StatusConflict)
			return
		}
		cost = rearmCost(hu.AmmoBins)
	}
	if err := s.store.SpendWarchest(r.Context(), pt.ID, c.Slug, cost); errors.Is(err, ErrInsufficientFunds) {
		http.Error(w, fmt.Sprintf("Not enough in the warchest: that costs %d SP and you have %d.", cost, run.Warchest), http.StatusConflict)
		return
	} else if err != nil {
		http.Error(w, "could not charge the warchest", http.StatusInternalServerError)
		return
	}
	if err := s.mekbay.ServiceUnit(r.Context(), ServiceRequest{OwnerUID: accountUID(currentSeed(r), SlotPlayer1), Instance: run.HangarInstance, UnitID: hu.ID, Kind: kind, Level: level, Tons: cat.Tons}); err != nil {
		log.Printf("%s a unit in MekBay: %v", kind, err) // never log the owner UID
		if rerr := s.store.RefundWarchest(r.Context(), pt.ID, c.Slug, cost); rerr != nil {
			log.Printf("could not refund %d SP: %v", cost, rerr)
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"cost":%d,"warchest":%d}`, cost, run.Warchest-cost)
}
