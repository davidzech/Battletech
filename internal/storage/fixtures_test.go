package storage

import (
	"context"

	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/pilot"
)

const (
	testAccount  = "0f1e2d3c-4b5a-4968-8776-655443322110"
	otherAccount = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
)

const (
	goodInstance = "01a12f9e-188f-7158-ad67-3fc9700ec733"
	goodSel      = "01a12f9d-a79b-717d-8c84-d5da73d2f9a5"
	// goodHangar is a link as a player pastes it, with a unit selection; the site keeps only goodHangarURL.
	goodHangar    = "https://mekbay.com/?instance=" + goodInstance + "&sel=" + goodSel
	goodHangarURL = "https://mekbay.com/?instance=" + goodInstance
)

// hangarOf builds a hangar of standard 4/5 units from MekBay unit IDs.
func hangarOf(names ...string) mekbay.HangarForce {
	h := mekbay.HangarForce{Name: "Test Hangar", Timestamp: "t"}
	for i, n := range names {
		h.Units = append(h.Units, mekbay.HangarUnit{ID: "u" + string(rune('a'+i)), Unit: n, Group: "Hangar", Gunnery: 4, Piloting: 5})
	}
	return h
}

// Shorthands the tests use, built on the methods the site calls.

// StartRun starts a campaign with its hangar and nothing else: no pilots, an empty warchest, no player count or force name.
func (s *Store) StartRun(ctx context.Context, playthroughID, campaign string, h mekbay.Hangar) error {
	return s.StartRunWithPilots(ctx, playthroughID, campaign, h, nil, 0, 0, "")
}

// AddPilot adds a pilot with the lowest rank and every SP allocated.
func (s *Store) AddPilot(ctx context.Context, playthroughID, campaign, name, portrait string, start pilot.SPAllocation, limit int) (pilot.NamedPilot, error) {
	return s.AddPilotWithRank(ctx, playthroughID, campaign, name, portrait, "", start, start.Total(), limit)
}

// AwardSP gives a living pilot SP already allocated to their columns.
func (s *Store) AwardSP(ctx context.Context, playthroughID, campaign, pilotID string, a pilot.SPAllocation) error {
	return s.ApplySP(ctx, playthroughID, campaign, pilotID, a.Total(), a)
}

// EarnSP gives a living pilot unallocated SP.
func (s *Store) EarnSP(ctx context.Context, playthroughID, campaign, pilotID string, n int) error {
	return s.ApplySP(ctx, playthroughID, campaign, pilotID, n, pilot.SPAllocation{})
}

// AllocateSP spends a living pilot's unallocated SP.
func (s *Store) AllocateSP(ctx context.Context, playthroughID, campaign, pilotID string, a pilot.SPAllocation) error {
	return s.ApplySP(ctx, playthroughID, campaign, pilotID, 0, a)
}
