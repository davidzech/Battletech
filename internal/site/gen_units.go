//go:build ignore

// gen_units.go builds units_index.json, the small slice of data/units.json the site needs at runtime
// (the full 27 MB catalog is not shipped in the Docker image), and availability.json, the units each campaign's faction can field in its era
// (Intro.md front matter availability_faction and availability_era, looked up in data/factions.json and data/eras.json, both MekBay's, by Master
// Unit List ID). Run it after refreshing data/units.json, factions.json or eras.json, or after a campaign names a new faction or era:
//
//	go generate ./internal/site
package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
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
	// MulID is the unit's Master Unit List ID, read for the availability lookup and never written.
	MulID int         `json:"mul1id,omitempty"`
	Ammo  []ammoEntry `json:"ammo,omitempty"`
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
	byMul := map[int]string{}
	for _, u := range doc.Units {
		if u.MulID > 0 {
			byMul[u.MulID] = u.Name
		}
	}
	for i := range doc.Units {
		doc.Units[i].MulID = 0
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
	writeAvailability(byMul)
}

// writeAvailability writes availability.json: for each "Faction|Era" pair a campaign names, the sorted names of the units available to that
// faction in that era.
func writeAvailability(byMul map[int]string) {
	var factions struct {
		Factions []struct {
			Name string           `json:"name"`
			Eras map[string][]int `json:"eras"`
		} `json:"factions"`
	}
	var eras struct {
		Eras []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"eras"`
	}
	readJSON("../../data/factions.json", &factions)
	readJSON("../../data/eras.json", &eras)
	eraID := map[string]string{}
	for _, e := range eras.Eras {
		eraID[e.Name] = strconv.Itoa(e.ID)
	}
	out := map[string][]string{}
	intros, _ := filepath.Glob("../../campaigns/*/Intro.md")
	for _, p := range intros {
		raw, err := os.ReadFile(p)
		if err != nil {
			log.Fatal(err)
		}
		parts := strings.SplitN(string(raw), "---", 3)
		if len(parts) < 3 {
			continue
		}
		var meta struct {
			Faction string `yaml:"availability_faction"`
			Era     string `yaml:"availability_era"`
		}
		if err := yaml.Unmarshal([]byte(parts[1]), &meta); err != nil || meta.Faction == "" || meta.Era == "" {
			continue
		}
		id, ok := eraID[meta.Era]
		if !ok {
			log.Fatalf("%s: unknown era %q", p, meta.Era)
		}
		found := false
		for _, f := range factions.Factions {
			if f.Name != meta.Faction {
				continue
			}
			found = true
			names := []string{}
			for _, mul := range f.Eras[id] {
				if n, ok := byMul[mul]; ok {
					names = append(names, n)
				}
			}
			sort.Strings(names)
			out[meta.Faction+"|"+meta.Era] = names
		}
		if !found {
			log.Fatalf("%s: unknown faction %q", p, meta.Faction)
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("availability.json", b, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote availability.json: %d faction and era pairs, %d bytes", len(out), len(b))
}

func readJSON(path string, into any) {
	raw, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		log.Fatalf("%s: %v", path, err)
	}
}
