package mekbay

import (
	"context"
	"strings"
	"time"
)

// HangarGroup is the name of the group a hangar force's units sit in, where a purchase adds new units.
const HangarGroup = "Hangar"

// SameGroupName compares group names the way the site reads them: ignoring case and surrounding space.
func SameGroupName(name, want string) bool {
	return strings.EqualFold(strings.TrimSpace(name), want)
}

// PurchaseRequest asks MekBay to add units to the player's hangar force.
type PurchaseRequest struct {
	OwnerUID string
	Instance string
	Units    []string // MekBay unit IDs (e.g. BMMadCat_H), one entry per unit bought
}

// addUnitsToForce adds a fresh, undamaged unit for each ID to the "Hangar" group of a saved force (the decoded JSON of its `data`), creating the
// group if it doesn't exist. Everything else in the force is left as it was.
func addUnitsToForce(force map[string]any, unitIDs []string, now time.Time) {
	groups, _ := force["groups"].([]any)
	var hangar map[string]any
	for _, g := range groups {
		if gm, ok := g.(map[string]any); ok {
			if name, _ := gm["name"].(string); hangar == nil && SameGroupName(name, HangarGroup) {
				hangar = gm
			}
		}
	}
	if hangar == nil {
		hangar = map[string]any{"id": NewID(), "name": HangarGroup, "color": "", "units": []any{}}
		groups = append(groups, hangar)
		force["groups"] = groups
	}
	units, _ := hangar["units"].([]any)
	for _, id := range unitIDs {
		units = append(units, map[string]any{
			"id": NewID(), "unit": id, "alias": "", "updatedTs": now.UnixMilli(),
			"state": map[string]any{
				"crew":      []any{map[string]any{"id": 0, "name": "", "gunnerySkill": 4, "pilotingSkill": 5, "hits": 0, "state": 0}},
				"crits":     []any{},
				"heat":      map[string]any{"current": 0, "previous": 0},
				"locations": map[string]any{},
				"modified":  false,
				"destroyed": false,
				"turnState": map[string]any{"applyMovePSR": false},
			},
		})
	}
	hangar["units"] = units
}

// PurchaseUnits adds units to the player's hangar force. It needs the owner's UID, because MekBay only accepts the owner's saves.
func (c *Client) PurchaseUnits(ctx context.Context, req PurchaseRequest) (HangarForce, error) {
	return c.editForce(ctx, req.OwnerUID, req.Instance, func(force map[string]any, now time.Time) error {
		addUnitsToForce(force, req.Units, now)
		return nil
	})
}
