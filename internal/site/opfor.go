package site

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/mekbay"
)

// A scenario's OpFor and operation as the battle builds them in MekBay (see battle.go).

// operationName is the operation's name in MekBay, e.g. "Refusal War - Scenario 001".
func operationName(c *content.Campaign, sc *content.Scenario) string {
	return c.Label + " - " + c.MissionNumber(*sc)
}

// opForDraft turns a scenario's .mul force files into one OpFor force, a group per file ("OpFor 1", "OpFor 2").
func opForDraft(c *content.Campaign, sc *content.Scenario) (mekbay.ForceDraft, error) {
	d := mekbay.ForceDraft{Name: operationName(c, sc) + " - OpFor"}
	for _, f := range sc.Forces {
		g, err := mulGroup(sc, f.File, f.Label)
		if err != nil {
			return mekbay.ForceDraft{}, err
		}
		d.Groups = append(d.Groups, g)
	}
	return d, nil
}

// alliesDraft turns a scenario's allies (Setup.md allies) into one force for the players' side, in one group; a scenario with none gives a draft with no groups.
func alliesDraft(c *content.Campaign, sc *content.Scenario) (mekbay.ForceDraft, error) {
	d := mekbay.ForceDraft{Name: operationName(c, sc) + " - Allies"}
	if len(sc.Allies) == 0 {
		return d, nil
	}
	all := mekbay.GroupDraft{Name: "Allies"}
	for _, f := range sc.Allies {
		g, err := mulGroup(sc, f, "Allies")
		if err != nil {
			return mekbay.ForceDraft{}, err
		}
		all.Units = append(all.Units, g.Units...)
	}
	d.Groups = []mekbay.GroupDraft{all}
	return d, nil
}

// mulGroup reads one .mul file of a scenario into a group of units, pilots and skills as the file gives them.
func mulGroup(sc *content.Scenario, file, label string) (mekbay.GroupDraft, error) {
	raw, err := os.ReadFile(filepath.Join(sc.Dir, file))
	if err != nil {
		return mekbay.GroupDraft{}, err
	}
	entities, err := content.ParseMul(raw)
	if err != nil {
		return mekbay.GroupDraft{}, fmt.Errorf("%s: %w", file, err)
	}
	g := mekbay.GroupDraft{Name: label}
	for _, e := range entities {
		u, ok := catalog.Lookup(e.Chassis, e.Model)
		if !ok {
			return mekbay.GroupDraft{}, fmt.Errorf("%s: %s %s is not in the unit catalog", file, e.Chassis, e.Model)
		}
		g.Units = append(g.Units, mekbay.UnitDraft{Unit: u.Name, BV: catalog.AdjustedBV(u.BV, e.Gunnery, e.Piloting), Pilot: e.Pilot, Gunnery: e.Gunnery, Piloting: e.Piloting})
	}
	return g, nil
}

// skipAllies is the forces a battle's write-back leaves out: its allies' force, if it has one.
func skipAllies(instance string) []string {
	if instance == "" {
		return nil
	}
	return []string{instance}
}
