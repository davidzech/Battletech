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

// operationName is the operation's name in MekBay, e.g. "Refusal War - Sortie 001".
func operationName(c *content.Campaign, sc *content.Scenario) string {
	return c.Label + " - " + c.MissionNumber(*sc)
}

// opForDraft turns a scenario's .mul force files into one OpFor force, a group per file ("OpFor 1", "OpFor 2").
func opForDraft(c *content.Campaign, sc *content.Scenario) (mekbay.ForceDraft, error) {
	d := mekbay.ForceDraft{Name: operationName(c, sc) + " - OpFor"}
	for _, f := range sc.Forces {
		raw, err := os.ReadFile(filepath.Join(sc.Dir, f.File))
		if err != nil {
			return mekbay.ForceDraft{}, err
		}
		entities, err := content.ParseMul(raw)
		if err != nil {
			return mekbay.ForceDraft{}, fmt.Errorf("%s: %w", f.File, err)
		}
		g := mekbay.GroupDraft{Name: f.Label}
		for _, e := range entities {
			u, ok := catalog.Lookup(e.Chassis, e.Model)
			if !ok {
				return mekbay.ForceDraft{}, fmt.Errorf("%s: %s %s is not in the unit catalog", f.File, e.Chassis, e.Model)
			}
			g.Units = append(g.Units, mekbay.UnitDraft{Unit: u.Name, BV: catalog.AdjustedBV(u.BV, e.Gunnery, e.Piloting), Pilot: e.Pilot, Gunnery: e.Gunnery, Piloting: e.Piloting})
		}
		d.Groups = append(d.Groups, g)
	}
	return d, nil
}
