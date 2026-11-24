package mekbay

import (
	"context"
	"errors"
	"time"
)

// removeUnitFromForce takes the unit with this ID out of a saved force (the decoded JSON of its `data`); false if it isn't there.
func removeUnitFromForce(force map[string]any, unitID string) bool {
	groups, _ := force["groups"].([]any)
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		units, _ := gm["units"].([]any)
		for i, u := range units {
			if um, _ := u.(map[string]any); um != nil && um["id"] == unitID {
				gm["units"] = append(units[:i:i], units[i+1:]...)
				return true
			}
		}
	}
	return false
}

// SellRequest asks MekBay to remove a unit from the site's copy of the hangar.
type SellRequest struct {
	OwnerUID string
	Instance string
	UnitID   string // the unit's own ID inside the force
}

// SellUnit removes a unit from the hangar force. It needs the owner's UID, because MekBay only accepts the owner's saves.
func (c *Client) SellUnit(ctx context.Context, req SellRequest) (HangarForce, error) {
	return c.editForce(ctx, req.OwnerUID, req.Instance, func(force map[string]any, _ time.Time) error {
		if !removeUnitFromForce(force, req.UnitID) {
			return errors.New("that unit is no longer in your hangar force")
		}
		return nil
	})
}
