package mekbay

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// CopyRequest asks MekBay to save a copy of one of the owner's forces under another instance ID (overwriting it if it exists), keeping everything on the units.
type CopyRequest struct {
	OwnerUID string
	Source   string
	Target   string
}

// CopyForce saves a copy of a force under a new instance ID, as it is (damage, ammunition and all).
func (c *Client) CopyForce(ctx context.Context, req CopyRequest) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := Dial(ctx, c.url(), req.OwnerUID)
	if err != nil {
		return err
	}
	defer s.Close()
	data, err := s.getForce(ctx, req.Source, false)
	if err != nil {
		return err
	}
	if data == nil {
		return errors.New("MekBay has no force to copy")
	}
	data["instanceId"] = req.Target
	return s.saveForce(ctx, data, time.Now())
}

// DeleteForce deletes forces of the owner's in MekBay (best effort: MekBay does not acknowledge a deletion).
func (c *Client) DeleteForce(ctx context.Context, owner string, instances ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if len(instances) == 0 {
		return nil
	}
	s, err := Dial(ctx, c.url(), owner)
	if err != nil {
		return err
	}
	defer s.Close()
	for _, in := range instances {
		if err := s.Send(ctx, map[string]any{"action": "delForce", "uuid": owner, "instanceId": in}); err != nil {
			return err
		}
	}
	// A read on the same connection comes back after the deletions have been handled, so closing the connection does not cut them off.
	_, _ = s.Call(ctx, map[string]any{"action": "getForce", "uuid": owner, "instanceId": instances[len(instances)-1], "ownedOnly": true})
	return nil
}

// DeleteBattle deletes a battle's operation and every force in it (the players' forces and the OpFor) in MekBay, best effort: MekBay does not acknowledge a
// deletion. It is how an aborted battle is cleaned up. It reads the operation first, so it works whatever the site still remembers of it.
func (c *Client) DeleteBattle(ctx context.Context, owner, operationID string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := Dial(ctx, c.url(), owner)
	if err != nil {
		return err
	}
	defer s.Close()
	raw, err := s.Call(ctx, map[string]any{"action": "getOperation", "operationId": operationID})
	if err != nil {
		return err
	}
	var op struct {
		Data *struct {
			Forces []struct {
				InstanceID string `json:"instanceId"`
			} `json:"forces"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &op); err != nil || op.Data == nil {
		return nil // already gone
	}
	for _, f := range op.Data.Forces {
		if err := s.Send(ctx, map[string]any{"action": "delForce", "uuid": owner, "instanceId": f.InstanceID}); err != nil {
			return err
		}
	}
	if err := s.Send(ctx, map[string]any{"action": "delOperation", "operationId": operationID}); err != nil {
		return err
	}
	_, _ = s.Call(ctx, map[string]any{"action": "getOperation", "operationId": operationID}) // after this the deletions have been handled
	return nil
}
