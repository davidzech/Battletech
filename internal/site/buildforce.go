package site

import (
	"context"
	"fmt"
	"net/http"

	"github.com/davidzech/Battletech/internal/mekbay"

	"github.com/davidzech/Battletech/internal/catalog"
)

// Building the starting hangar on the site. Instead of linking a force they made in MekBay, the player picks the mechs of their starting force from the same unit
// search the Purchase dialog uses (the start wizard's "Build your force"), and the site makes the hangar itself: a new force in MekBay owned by the account derived
// from the seed, every mech pristine with an unnamed 4/5 pilot, exactly what a cloned hangar starts as. Linking a MekBay hangar stays as a shortcut for a force
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

// buildHangar makes the starting hangar from the picked mechs: a new force in MekBay owned by the seed's account, in one group named Hangar. The run's hangar then
// points at it, and since nothing was copied from a link of the player's, the force's own link stands as its source.
func (s *Server) buildHangar(ctx context.Context, seed, name string, ids []string) (mekbay.Hangar, mekbay.HangarForce, error) {
	units := make([]mekbay.UnitDraft, len(ids))
	for i, id := range ids {
		units[i] = mekbay.UnitDraft{Unit: id, Gunnery: mekbay.DefaultGunnery, Piloting: mekbay.DefaultPiloting}
		if u, ok := catalog.ByName(id); ok {
			units[i].BV = u.BV // the force's BV as MekBay would total it (a 4/5 pilot leaves the base BV as it is)
		}
	}
	target := mekbay.NewID()
	got, err := s.mekbay.CreateForce(ctx, mekbay.AccountUID(seed, mekbay.SlotHangar), target, mekbay.ForceDraft{Name: name, Groups: []mekbay.GroupDraft{{Name: mekbay.HangarGroup, Units: units}}})
	if err != nil {
		return mekbay.Hangar{}, mekbay.HangarForce{}, err
	}
	link := "https://mekbay.com/?instance=" + target
	return mekbay.Hangar{URL: link, Instance: target, Source: link}, got, nil
}
