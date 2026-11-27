package storage

import (
	"context"

	"github.com/davidzech/Battletech/internal/pilot"
)

const (
	testAccount  = "0f1e2d3c-4b5a-4968-8776-655443322110"
	otherAccount = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
)

const (
	goodInstance = "01a12f9e-188f-7158-ad67-3fc9700ec733"
)

// testHangar is a stored hangar: an empty force with this name.
func testHangar(name string) []byte { return []byte(`{"name":"` + name + `","groups":[]}`) }

// sameHangar reports whether a run's stored hangar is testHangar(name).
func sameHangar(s *Store, ctx context.Context, playthroughID, campaign, name string) bool {
	h, err := s.Hangar(ctx, playthroughID, campaign)
	return err == nil && string(h) == string(testHangar(name))
}

// Shorthands the tests use, built on the methods the site calls.

// StartRun starts a campaign with its hangar and nothing else: no pilots, an empty warchest, no player count or force name.
func (s *Store) StartRun(ctx context.Context, playthroughID, campaign string, h []byte) error {
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
