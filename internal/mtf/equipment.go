package mtf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Equipment is what laying out a design needs to know of one piece of equipment, from MekBay's equipment catalog (online-assets/static/equipment.json,
// MegaMek's equipment as MekBay publishes it).
type Equipment struct {
	ID       string // MegaMek's internal name, the catalog's key
	Name     string // display name, e.g. "LRM 20 Ammo"
	Type     string // weapon, ammo, misc, structure, armor
	TechBase string // IS, Clan or All
	// Slots is the number of critical slots it takes; 0 when the catalog says "variable" (it depends on the design).
	Slots      int
	Spreadable bool // its slots may sit in several locations and are one component (Endo Steel, Ferro-Fibrous ...)
	CanSplit   bool // a weapon of 8 or more slots, or equipment flagged to split, may span two adjacent locations
	TC         bool // a targeting computer: one component owns all its slots, wherever they are
	PerTon     bool // cargo, liquid storage and communications equipment: one slot per ton of the size its row gives (supportEquipmentCriticalSlots)
	Shots      int  // shots in one ton (ammunition only)
	Aliases    []string
}

// Equipments is MekBay's equipment catalog indexed as its EquipmentRegistry indexes it (equipment-lookup.ts), so a critical slot's name finds the same
// equipment in both.
type Equipments struct {
	byID     map[string]*Equipment
	internal map[string]*Equipment   // lowercase internal name → the first such
	aliases  map[string]*Equipment   // lowercase alias → the last such
	variants map[string][]*Equipment // lowercase internal name or alias → every match, in catalog order
	list     []*Equipment
}

// catalogEntry is one entry of MekBay's equipment.json, the parts read here.
type catalogEntry struct {
	Type    string   `json:"type"`
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
	Flags   []string `json:"flags"`
	Stats   struct {
		CriticalSlots any  `json:"criticalSlots"`
		Spreadable    bool `json:"spreadable"`
	} `json:"stats"`
	Tech struct {
		Base string `json:"base"`
	} `json:"tech"`
	Ammo *struct {
		Shots int `json:"shots"`
	} `json:"ammo"`
}

// ReadEquipments reads MekBay's equipment.json ({"version", "equipment": {<internal name>: {...}}}), keeping the catalog's order, which decides between two
// entries whose names differ only in case.
func ReadEquipments(r io.Reader) (*Equipments, error) {
	var root struct {
		Equipment json.RawMessage `json:"equipment"`
	}
	if err := json.NewDecoder(r).Decode(&root); err != nil {
		return nil, fmt.Errorf("could not read the equipment catalog: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(root.Equipment))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, fmt.Errorf("the equipment catalog has no equipment object")
	}
	var list []*Equipment
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("could not read the equipment catalog: %w", err)
		}
		key, _ := t.(string)
		var e catalogEntry
		if err := dec.Decode(&e); err != nil {
			return nil, fmt.Errorf("equipment %q: %w", key, err)
		}
		eq := &Equipment{ID: key, Name: e.Name, Type: e.Type, TechBase: e.Tech.Base, Spreadable: e.Stats.Spreadable, Aliases: e.Aliases}
		if n, ok := e.Stats.CriticalSlots.(float64); ok {
			eq.Slots = int(n)
		}
		for _, f := range e.Flags {
			switch f {
			case "F_CAN_BE_SPlIT_ACROSS_CRITICAL_SLOTS": // sic, as MegaMek spells it
				eq.CanSplit = true
			case "F_TARGETING_COMPUTER":
				eq.TC = true
			case "F_CARGO", "F_LIQUID_CARGO", "F_COMMUNICATIONS":
				eq.PerTon = true
			}
		}
		if e.Type == "weapon" && eq.Slots >= 8 {
			eq.CanSplit = true
		}
		if e.Ammo != nil {
			eq.Shots = e.Ammo.Shots
		}
		list = append(list, eq)
	}
	return NewEquipments(list), nil
}

// NewEquipments indexes a list of equipment, in catalog order.
func NewEquipments(list []*Equipment) *Equipments {
	es := &Equipments{byID: map[string]*Equipment{}, internal: map[string]*Equipment{}, aliases: map[string]*Equipment{}, variants: map[string][]*Equipment{}, list: list}
	addVariant := func(key string, e *Equipment) {
		for _, v := range es.variants[key] {
			if v == e {
				return
			}
		}
		es.variants[key] = append(es.variants[key], e)
	}
	for _, e := range list {
		es.byID[e.ID] = e
		key := lookupKey(e.ID)
		if _, ok := es.internal[key]; !ok {
			es.internal[key] = e
		}
		addVariant(key, e)
	}
	for _, e := range list {
		for _, a := range e.Aliases {
			key := lookupKey(a)
			es.aliases[key] = e
			addVariant(key, e)
		}
	}
	return es
}

// All lists the equipment in catalog order.
func (es *Equipments) All() []*Equipment { return es.list }

func lookupKey(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// Find finds the equipment a critical slot names, for a design of this tech base ("IS" or "Clan"), as MekBay's findForTechBase does: the exact internal
// name, then the internal name whatever its case, then a match of that tech base (or of both), then any alias. Nil when nothing matches.
func (es *Equipments) Find(name, techBase string) *Equipment {
	if es == nil || name == "" {
		return nil
	}
	if e := es.byID[name]; e != nil {
		return e
	}
	key := lookupKey(name)
	if e := es.internal[key]; e != nil {
		return e
	}
	for _, want := range []string{techBase, "All"} {
		for _, e := range es.variants[key] {
			if e.TechBase == want {
				return e
			}
		}
	}
	return es.aliases[key]
}
