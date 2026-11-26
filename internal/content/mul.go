package content

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/davidzech/Battletech/internal/catalog"
)

// MulEntity is one unit from a MegaMek/MekBay .mul force file.
type MulEntity struct {
	Chassis  string
	Model    string
	Pilot    string
	Gunnery  int
	Piloting int
}

// ParseMul reads the units out of a .mul file, in file order. Every unit must give its pilot's gunnery and piloting: a unit without them would otherwise
// read as an elite 0/0 pilot, so it is an error instead (a real 0 has to be written out).
func ParseMul(data []byte) ([]MulEntity, error) {
	var doc struct {
		Entities []struct {
			Chassis string `xml:"chassis,attr"`
			Model   string `xml:"model,attr"`
			Pilot   *struct {
				Name     string `xml:"name,attr"`
				Gunnery  *int   `xml:"gunnery,attr"`
				Piloting *int   `xml:"piloting,attr"`
			} `xml:"pilot"`
		} `xml:"entity"`
	}
	if err := xml.NewDecoder(bytes.NewReader(data)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("read .mul: %w", err)
	}
	out := make([]MulEntity, 0, len(doc.Entities))
	for i, e := range doc.Entities {
		if e.Pilot == nil || e.Pilot.Gunnery == nil || e.Pilot.Piloting == nil {
			return nil, fmt.Errorf("read .mul: unit %d (%s) needs a <pilot> with gunnery and piloting", i+1, strings.TrimSpace(e.Chassis+" "+e.Model))
		}
		out = append(out, MulEntity{Chassis: e.Chassis, Model: e.Model, Pilot: e.Pilot.Name, Gunnery: *e.Pilot.Gunnery, Piloting: *e.Pilot.Piloting})
	}
	return out, nil
}

// forceProblems checks a scenario's front matter `forces` when the campaign loads, so a mistake shows on the campaign page instead of when a battle starts:
// an entry that isn't a plain file name in the scenario's folder (forceFiles leaves those out), a file that can't be read or parsed, and a unit the
// catalog doesn't know.
func forceProblems(dir string, m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var problems []string
	for _, k := range keys {
		f := m[k]
		if f == "" || f != filepath.Base(f) {
			problems = append(problems, fmt.Sprintf("forces: %s: %q must be a file in the scenario's folder", k, f))
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			problems = append(problems, fmt.Sprintf("forces: %s can't be read: %v", f, err))
			continue
		}
		entities, err := ParseMul(raw)
		if err != nil {
			problems = append(problems, fmt.Sprintf("forces: %s: %v", f, err))
			continue
		}
		for _, e := range entities {
			if _, ok := catalog.Lookup(e.Chassis, e.Model); !ok {
				problems = append(problems, fmt.Sprintf("forces: %s: %s %s is not in the unit catalog", f, e.Chassis, e.Model))
			}
		}
	}
	return problems
}
