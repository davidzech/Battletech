//go:build ignore

// gen_units.go builds units_index.json, the small slice of data/units.json the site needs at runtime
// (the full 27 MB catalog is not shipped in the Docker image), and availability.json, the units each campaign's faction can field in its era
// (Intro.md front matter availability_faction and availability_era, looked up in data/factions.json and data/eras.json, both MekBay's, by Master
// Unit List ID). Run it after refreshing data/units.json, factions.json or eras.json, or after a campaign names a new faction or era:
//
//	go generate ./internal/catalog
package main

import (
	"encoding/json"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type entry struct {
	Name    string  `json:"name"`
	UUID    string  `json:"uuid"` // MegaMek's own ID for the design, which MekBay's version 2 forces name units by
	Chassis string  `json:"chassis"`
	Model   string  `json:"model"`
	BV      int     `json:"bv"`
	Tons    float64 `json:"tons"`
	Class   string  `json:"weightClass"`
	Role    string  `json:"role"`
	Type    string  `json:"type"`
	Tech    string  `json:"techBase"`
	Omni    int     `json:"omni,omitempty"`   // 1 for an omnimech, whose variants can be swapped (see swap.go)
	Engine  string  `json:"engine,omitempty"` // Fusion, XL (Clan), ...; the crippling rule needs to know Fusion from the rest
	Icon    string  `json:"icon,omitempty"`   // in data/units.json: the path inside mm-data; written to the index as iconFile(path)
	// A mech's construction, for the unit card (card.go); left out for anything else to keep the index small.
	Armor         int    `json:"armor,omitempty"`         // armor points in all
	ArmorType     string `json:"armorType,omitempty"`     // e.g. "Ferro-Fibrous"
	Internal      int    `json:"internal,omitempty"`      // internal structure points in all
	StructureType string `json:"structureType,omitempty"` // e.g. "Endo Steel"
	Heat          int    `json:"heat,omitempty"`          // heat generated firing everything
	Dissipation   int    `json:"dissipation,omitempty"`   // heat sunk per turn
	Walk          int    `json:"walk,omitempty"`
	Run           int    `json:"run,omitempty"`
	Jump          int    `json:"jump,omitempty"`
	// Firepower, MaxRange and Range are worked out from the weapons the way MekBay's own stat bars do (unit-search-index.service.ts,
	// unit-range.util.ts): Firepower is the damage of every weapon fired at once, physical attacks left out; MaxRange the longest weapon range in hexes;
	// Range the range weighted by each weapon's damage, rounded to the nearest of the weapons' own ranges.
	Firepower int `json:"firepower,omitempty"`
	MaxRange  int `json:"maxRange,omitempty"`
	Range     int `json:"range,omitempty"`
	// Ballistic, Energy and Missile count the mech's weapons of each type (the components typed B, E and M, by quantity), as MekBay's weapon type bar.
	Ballistic int `json:"ballistic,omitempty"`
	Energy    int `json:"energy,omitempty"`
	Missile   int `json:"missile,omitempty"`
	// CockpitLoc is where a mech's cockpit is when it is not in the head (a torso-mounted cockpit: "CT"); "" for the usual head cockpit, and for anything else.
	CockpitLoc string `json:"cockpitLoc,omitempty"`
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
	Type string `json:"t"`  // P for a physical weapon
	R    string `json:"r"`  // ranges in hexes, e.g. "5/10/15"
	MD   string `json:"md"` // maximum damage, e.g. "7.0"
}

// weaponStats works out a mech's Firepower, MaxRange and Range from its components, as MekBay does (see entry).
func weaponStats(comp []compEntry) (firepower, maxRange, weighted int) {
	type part struct {
		damage float64
		known  bool
		qty    int
		rng    int
	}
	var parts []part
	sum := 0.0
	for _, c := range comp {
		qty := c.Q
		if qty == 0 {
			qty = 1
		}
		md, err := strconv.ParseFloat(strings.TrimSpace(c.MD), 64)
		known := err == nil && strings.TrimSpace(c.MD) != ""
		if known && c.Type != "P" {
			sum += md * float64(qty)
		}
		r := 0
		for _, x := range strings.Split(c.R, "/") {
			if n, err := strconv.Atoi(strings.TrimSpace(x)); err == nil && n > r {
				r = n
			}
		}
		if r > 0 {
			parts = append(parts, part{md, known, qty, r})
			maxRange = max(maxRange, r)
		}
	}
	firepower = int(math.Round(sum))
	if len(parts) == 0 {
		return firepower, 0, 0
	}
	total, count := 0.0, 0
	for _, p := range parts {
		if p.known {
			total += p.damage * float64(p.qty)
			count += p.qty
		}
	}
	avg := 0.0
	if count > 0 {
		avg = total / float64(count)
	}
	weight, ranged := 0.0, 0.0
	for _, p := range parts {
		d := p.damage
		if !p.known {
			d = avg
		}
		if w := d * float64(p.qty); w > 0 {
			weight += w
			ranged += float64(p.rng) * w
		}
	}
	if weight <= 0 {
		return firepower, maxRange, 0
	}
	exact := math.Round(ranged/weight*100) / 100
	// The nearest of the weapons' own ranges, the longer on a tie.
	best, bestDist := -1, 0.0
	for _, p := range parts {
		d := math.Abs(exact - float64(p.rng))
		if best < 0 || d < bestDist || (d == bestDist && p.rng > best) {
			best, bestDist = p.rng, d
		}
	}
	return firepower, maxRange, best
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
		if doc.Units[i].Type == "Mek" {
			u := &doc.Units[i]
			u.Firepower, u.MaxRange, u.Range = weaponStats(u.Comp)
			for _, c := range u.Comp {
				switch c.Type {
				case "B":
					u.Ballistic += max(c.Q, 1)
				case "E":
					u.Energy += max(c.Q, 1)
				case "M":
					u.Missile += max(c.Q, 1)
				}
				if c.ID == "cockpit" && c.Loc != "" && c.Loc != "HD" {
					u.CockpitLoc = c.Loc
				}
			}
		}
		doc.Units[i].Comp = nil
		if doc.Units[i].Type != "Mek" {
			u := &doc.Units[i]
			u.Armor, u.ArmorType, u.Internal, u.StructureType, u.Heat, u.Dissipation, u.Walk, u.Run, u.Jump = 0, "", 0, "", 0, 0, 0, 0, 0
		}
		// Mechs, vehicles and battle armor have vendored icons (see gen_icons.go); anything else shows a default one. A mech whose catalog icon is in a folder
		// that isn't vendored (BMHammerhead_2 points at the Hammerhead fighter's) takes the mech icon of the same name, if there is one.
		switch ic := doc.Units[i].Icon; {
		case vendoredIcon(ic):
			doc.Units[i].Icon = iconFile(ic)
		case doc.Units[i].Type == "Mek" && ic != "":
			doc.Units[i].Icon = iconFile("meks/" + ic[strings.LastIndex(ic, "/")+1:])
		default:
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
