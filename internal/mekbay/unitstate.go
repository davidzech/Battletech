package mekbay

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/davidzech/Battletech/internal/catalog"
)

// What a battle's unit state becomes once it is back in the hangar. MekBay's record sheet keeps damage pending until the phase ends (endPhase in
// cbt-force-unit-state.model.ts consolidates it), and a battle is often reported right after the last shots, with the phase still open, so the write-back makes
// the same consolidation itself: pending damage counts. It then drops what only means something on the table (heat, the turn's state, the unit's battle
// conditions, NARC pods), and sets the destroyed flag as MekBay's evaluateDestroyed would. The rules here follow MekBay's source; see each function.

// settleBattleState consolidates the state (the decoded JSON of its `state`) of a battle's unit (unit, its MekBay unit ID) and clears its battle leftovers, in
// place. A unit the catalog doesn't know has no known structure, so only the slots MekBay already marked count as destroyed with their location.
func settleBattleState(state map[string]any, unit any, now time.Time) {
	tons := unitTons(unit)
	consolidateLocations(state)
	consolidateCrits(state, tons, now)
	consolidateInventory(state)
	// Battle leftovers. Heat (with any pending next value, and the heat sinks switched off) starts cold; the turn's state (movement, PSRs, pending events) is the
	// table's; every unit condition MekBay has (shutdown, prone, swarmed, tagged, ECM shielded, skidding, jammed, out of control, random movement, and the
	// derived ones: UNIT_CONDITION_DEFINITIONS in unit-type-rules.ts) describes the unit's situation in a battle, not its repair state.
	state["heat"] = map[string]any{"current": 0, "previous": 0}
	delete(state, "turnState")
	delete(state, "conditions")
	// MekBay's evaluateDestroyed (mek-rules.ts) sets the flag when three engine slots are gone or every crew cockpit is. The flag is only ever set here, never
	// cleared: a unit MekBay marked destroyed stays so until the destroyed-level repair. IsDestroyed reads the same rule, so the stored flag and what the site
	// shows agree.
	if hu := unitView(state, unit); hu.EngineHits >= engineDestructionHits || hu.CockpitGone {
		state["destroyed"] = true
	}
}

// engineDestructionHits is how many engine slots destroy a mech (MekRules.ENGINE_DESTRUCTION_HITS).
const engineDestructionHits = 3

// consolidateLocations is MekBay's consolidateLocations: each location's pending armor and structure loss is added to what it had lost, and its pending
// conditions are committed. NARC pods are removed: they are attached during a battle and mean nothing after it (MekBay itself removes them from a destroyed
// location, clearNarcFromCommittedPhysicallyDestroyedLocations). A location left with nothing is dropped, as MekBay saves only damaged ones.
func consolidateLocations(state map[string]any) {
	locs, _ := state["locations"].(map[string]any)
	for k, v := range locs {
		m, _ := v.(map[string]any)
		if m == nil {
			delete(locs, k)
			continue
		}
		m["armor"] = numberOf(m["armor"]) + numberOf(m["pendingArmor"])
		m["internal"] = numberOf(m["internal"]) + numberOf(m["pendingInternal"])
		delete(m, "pendingArmor")
		delete(m, "pendingInternal")
		var kept []any
		cs, _ := m["conditions"].([]any)
		for _, c := range cs {
			switch c := c.(type) {
			case string:
				if c != "narc" {
					kept = append(kept, c)
				}
			case map[string]any:
				key, _ := c["key"].(string)
				if key == "" || key == "narc" || c["remove"] == true {
					continue
				}
				// committedConditionData: a committed condition keeps only its value.
				if v := numberOf(c["value"]); v != 0 {
					kept = append(kept, map[string]any{"key": key, "value": v})
				} else {
					kept = append(kept, key)
				}
			}
		}
		if len(kept) > 0 {
			m["conditions"] = kept
		} else {
			delete(m, "conditions")
		}
		if numberOf(m["armor"]) == 0 && numberOf(m["internal"]) == 0 && len(kept) == 0 {
			delete(locs, k)
		}
	}
}

// consolidateCrits is MekBay's consolidateCrits, after its evaluateDestroyed has marked the slots that go with their location: a slot in a location whose
// internal structure is gone, or hit as often as it can take (twice if armored, else once), is destroyed; pending hits are added to the hits (their timestamps
// merged); and a slot being destroyed (destroying) is destroyed. Only the slots the state lists can be marked: MekBay saves a slot only once something happened
// to it, and its own app marks a destroyed location's slots as it happens.
func consolidateCrits(state map[string]any, tons float64, now time.Time) {
	locs, _ := state["locations"].(map[string]any)
	structurallyGone := func(loc string) bool {
		m, _ := locs[loc].(map[string]any)
		max := maxStructure(loc, tons)
		return m != nil && max > 0 && numberOf(m["internal"]) >= max
	}
	crits, _ := state["crits"].([]any)
	for _, c := range crits {
		cm, _ := c.(map[string]any)
		if cm == nil {
			continue
		}
		if p := numberOf(cm["pendingHits"]); p != 0 {
			hits := numberOf(cm["hits"])
			stamps := floats(cm["hitTimestamps"])
			if p > 0 {
				stamps = append(stamps, floats(cm["pendingHitTimestamps"])...)
				sort.Float64s(stamps)
			} else if len(stamps) > 0 {
				stamps = stamps[:max(0, len(stamps)+p)]
			}
			if len(stamps) > 0 {
				cm["hitTimestamps"] = stamps
			} else {
				delete(cm, "hitTimestamps")
			}
			cm["hits"] = max(0, hits+p)
		}
		delete(cm, "pendingHits")
		delete(cm, "pendingHitTimestamps")
		loc, _ := cm["loc"].(string)
		maxHits := 1
		if cm["armored"] == true {
			maxHits = 2
		}
		if cm["destroying"] == nil && (structurallyGone(loc) || numberOf(cm["hits"]) >= maxHits) {
			cm["destroying"] = now.UnixMilli()
		}
		if d := cm["destroying"]; d != nil {
			if cm["destroyed"] == nil {
				cm["destroyed"] = d
			}
		} else if cm["destroyed"] != nil {
			// MekBay commits destroyed = destroying: a slot no longer being destroyed (healed before the phase ended) is not destroyed.
			delete(cm, "destroyed")
			delete(cm, "destroyedTurn")
		}
	}
}

// consolidateInventory is MekBay's consolidateInventory: an item's pending destroyed state (destroying) becomes its destroyed state.
func consolidateInventory(state map[string]any) {
	inv, _ := state["inventory"].([]any)
	for _, it := range inv {
		im, _ := it.(map[string]any)
		if im == nil {
			continue
		}
		if d, ok := im["destroying"].(bool); ok {
			if d {
				im["destroyed"] = true
			} else {
				delete(im, "destroyed")
			}
		}
		delete(im, "destroying")
		if im["destroyed"] == false {
			delete(im, "destroyed")
		}
	}
}

// unitView reads one unit's state (the decoded JSON of its `state`) as the site sees it, for a rule that needs the unit's condition while editing it. unit is
// its MekBay unit ID (for where its cockpit is).
func unitView(state map[string]any, unit any) HangarUnit {
	b, _ := json.Marshal(state)
	var st unitState
	_ = json.Unmarshal(b, &st)
	name, _ := unit.(string)
	return st.view("", name, "")
}

// unitTons is a unit's tonnage from the catalog (0 if unknown).
func unitTons(unit any) float64 {
	name, _ := unit.(string)
	if u, ok := catalog.ByName(name); ok {
		return u.Tons
	}
	return 0
}

// numberOf reads a JSON number as an int, whichever Go type it has (0 for anything else).
func numberOf(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}

// floats reads a JSON array of numbers.
func floats(v any) []float64 {
	list, _ := v.([]any)
	out := make([]float64, 0, len(list))
	for _, x := range list {
		switch n := x.(type) {
		case float64:
			out = append(out, n)
		case int:
			out = append(out, float64(n))
		case int64:
			out = append(out, float64(n))
		}
	}
	return out
}
