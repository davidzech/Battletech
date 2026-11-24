package mekbay

import (
	"context"
	"errors"
	"time"
)

// swapUnitState reconfigures the unit with this ID inside a force in MekBay's save format: its design becomes newName and its state is reset (a fresh unit has no
// damage, no critical hits and full ammunition, and the old variant's slots mean nothing to the new one).
func swapUnitState(force map[string]any, unitID, newName string, now time.Time) error {
	groups, _ := force["groups"].([]any)
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		units, _ := gm["units"].([]any)
		for _, u := range units {
			um, _ := u.(map[string]any)
			if um == nil || um["id"] != unitID {
				continue
			}
			pristineUnit(um, now)
			state, _ := um["state"].(map[string]any)
			state["crits"] = []any{}
			um["unit"] = newName
			return nil
		}
	}
	return errors.New("that unit is no longer in your hangar force")
}

// SwapRequest asks MekBay to reconfigure a unit of the site's copy of the hangar.
type SwapRequest struct {
	OwnerUID string
	Instance string
	UnitID   string // the unit's own ID inside the force
	To       string // MekBay unit ID of the variant
}

// SwapUnit reconfigures a unit in the hangar force. It needs the owner's UID, because MekBay only accepts the owner's saves.
func (c *Client) SwapUnit(ctx context.Context, req SwapRequest) (HangarForce, error) {
	return c.editForce(ctx, req.OwnerUID, req.Instance, func(force map[string]any, now time.Time) error {
		return swapUnitState(force, req.UnitID, req.To, now)
	})
}
