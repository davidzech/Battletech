package mekbay

import (
	"encoding/json"
	"fmt"
	"time"
)

// DraftHangar is a list of picked mechs as a hangar force, for the starting check before the hangar is built.
func DraftHangar(ids []string) HangarForce {
	h := HangarForce{Name: "Hangar"}
	for i, id := range ids {
		h.Units = append(h.Units, HangarUnit{ID: fmt.Sprintf("pick-%d", i), Unit: id, Group: HangarGroup, Gunnery: DefaultGunnery, Piloting: DefaultPiloting})
	}
	return h
}

// NewHangar builds a new hangar force from a draft (the starting hangar built on the site, see internal/site/buildforce.go): its saved data as JSON, every unit
// pristine.
func NewHangar(d ForceDraft, now time.Time) ([]byte, error) {
	f, err := buildDraft(NewID(), now.UTC(), d)
	if err != nil {
		return nil, err
	}
	return json.Marshal(f)
}

// buildDraft builds a pristine force from a draft in the format the site writes (see V2).
func buildDraft(instance string, now time.Time, d ForceDraft) (any, error) {
	if V2() {
		return BuildForceV2(instance, now, d)
	}
	return BuildForce(instance, now, d), nil
}
