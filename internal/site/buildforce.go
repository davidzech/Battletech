package site

import (
	"fmt"
	"net/http"
	"time"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/mekbay"
)

// Building the starting hangar on the site. Instead of linking a force they made in MekBay, the player picks the mechs of their starting force from the same unit
// search the Purchase dialog uses (the start wizard's "Build your force"), and the site makes the hangar itself, in MekBay's save format: every mech pristine with
// an unnamed 4/5 pilot, exactly what a hangar imported from a link starts as. Linking a MekBay hangar stays as a shortcut for a force
// already built there.

// maxStartingUnits is the most mechs a starting force may be built from (a typo guard; the campaign's starting BV is the real limit).
const maxStartingUnits = 40

// parseStartingUnits reads the mechs picked for the starting force: the repeated `unit` fields, MekBay unit IDs (e.g. BMMadCat_H); a design may be picked more
// than once. Nil when none were sent (the player linked a hangar instead).
func parseStartingUnits(r *http.Request) ([]string, error) {
	ids := r.Form["unit"]
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > maxStartingUnits {
		return nil, fmt.Errorf("A starting force can hold at most %d mechs.", maxStartingUnits)
	}
	for _, id := range ids {
		if u, ok := catalog.ByName(id); !ok || u.Type != "Mek" {
			return nil, fmt.Errorf("%q is not a mech in the unit catalog.", id)
		}
	}
	return ids, nil
}

// buildHangar makes the starting hangar from the picked mechs: a new force in one group named Hangar, as the run's hangar stores it.
func buildHangar(name string, ids []string) ([]byte, error) {
	units := make([]mekbay.UnitDraft, len(ids))
	for i, id := range ids {
		units[i] = mekbay.UnitDraft{Unit: id, Gunnery: mekbay.DefaultGunnery, Piloting: mekbay.DefaultPiloting}
		if u, ok := catalog.ByName(id); ok {
			units[i].BV = u.BV // the force's BV as MekBay would total it (a 4/5 pilot leaves the base BV as it is)
		}
	}
	return mekbay.NewHangar(mekbay.ForceDraft{Name: name, Groups: []mekbay.GroupDraft{{Name: mekbay.HangarGroup, Units: units}}}, time.Now())
}
