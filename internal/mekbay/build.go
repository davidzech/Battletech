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
	return json.Marshal(BuildForce(NewID(), now.UTC(), d))
}
