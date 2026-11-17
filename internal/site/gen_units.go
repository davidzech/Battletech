//go:build ignore

// gen_units.go builds units_index.json, the small slice of data/units.json the site needs at runtime
// (the full 27 MB catalog is not shipped in the Docker image). Run it after refreshing data/units.json:
//
//	go generate ./internal/site
package main

import (
	"encoding/json"
	"log"
	"os"
	"sort"
	"strings"
)

type entry struct {
	Name    string  `json:"name"`
	Chassis string  `json:"chassis"`
	Model   string  `json:"model"`
	BV      int     `json:"bv"`
	Tons    float64 `json:"tons"`
	Class   string  `json:"weightClass"`
	Role    string  `json:"role"`
	Type    string  `json:"type"`
	Tech    string  `json:"techBase"`
	Engine  string  `json:"engine,omitempty"` // Fusion, XL (Clan), ...; the crippling rule needs to know Fusion from the rest
	Icon    string  `json:"icon,omitempty"`   // in data/units.json: the path inside mm-data; written to the index as iconFile(path)
	// Comp is read from data/units.json (the unit's components) and never written; the index keeps only the ammo among them, as Ammo.
	Comp []compEntry `json:"comp,omitempty"`
	Ammo []ammoEntry `json:"ammo,omitempty"`
}

type compEntry struct {
	ID   string `json:"id"`
	Name string `json:"n"`
	Loc  string `json:"l"`
	Q    int    `json:"q"`  // slots (tons)
	Q2   int    `json:"q2"` // total shots
}

// ammoEntry is one ammo entry of a unit: the bins of one ammo type in one location. Slots are tons, and Shots is the total over all of them.
type ammoEntry struct {
	ID    string `json:"id"`
	Name  string `json:"n"`
	Loc   string `json:"l"`
	Slots int    `json:"q"`
	Shots int    `json:"s"`
}

func main() {
	raw, err := os.ReadFile("../../data/units.json")
	if err != nil {
		log.Fatal(err)
	}
	var doc struct {
		Units []entry `json:"units"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		var list []entry
		if err2 := json.Unmarshal(raw, &list); err2 != nil {
			log.Fatal(err)
		}
		doc.Units = list
	}
	for i := range doc.Units {
		for _, c := range doc.Units[i].Comp {
			if doc.Units[i].Type == "Mek" && strings.Contains(c.Name, "Ammo") && c.Q > 0 {
				doc.Units[i].Ammo = append(doc.Units[i].Ammo, ammoEntry{ID: c.ID, Name: c.Name, Loc: c.Loc, Slots: c.Q, Shots: c.Q2})
			}
		}
		doc.Units[i].Comp = nil
		// Only mechs have vendored icons (see gen_icons.go); anything else shows a default one.
		if doc.Units[i].Type == "Mek" && doc.Units[i].Icon != "" {
			doc.Units[i].Icon = iconFile(doc.Units[i].Icon)
		} else {
			doc.Units[i].Icon = ""
		}
	}
	sort.Slice(doc.Units, func(i, j int) bool { return doc.Units[i].Name < doc.Units[j].Name })
	out, err := json.Marshal(doc.Units)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("units_index.json", out, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote units_index.json: %d units, %d bytes", len(doc.Units), len(out))
}
