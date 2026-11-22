package mekbay

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// DraftHangar is a list of picked mechs as a hangar force, for the starting check before anything is made in MekBay.
func DraftHangar(ids []string) HangarForce {
	h := HangarForce{Name: "Hangar"}
	for i, id := range ids {
		h.Units = append(h.Units, HangarUnit{ID: fmt.Sprintf("pick-%d", i), Unit: id, Group: HangarGroup, Gunnery: DefaultGunnery, Piloting: DefaultPiloting})
	}
	return h
}

// CreateForce saves a new force, built from a draft, under the owner's account at the given instance ID, and returns it as the site reads a hangar.
func (c *Client) CreateForce(ctx context.Context, owner, instance string, d ForceDraft) (HangarForce, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := Dial(ctx, c.url(), owner)
	if err != nil {
		return HangarForce{}, err
	}
	defer s.Close()
	force := BuildForce(instance, time.Now().UTC(), d)
	if err := s.saveData(ctx, force); err != nil {
		return HangarForce{}, err
	}
	b, err := json.Marshal(map[string]any{"data": force})
	if err != nil {
		return HangarForce{}, err
	}
	return ParseHangarForce(b)
}
