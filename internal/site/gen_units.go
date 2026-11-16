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
