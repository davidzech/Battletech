package storage

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/davidzech/Battletech/internal/mekbay"
)

// TestHistoryBackfillsTheStart: a campaign started before the history existed is given its start line when the store opens, once, as line 0, which a rollback
// never takes away.
func TestHistoryBackfillsTheStart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	const id = "pt-backfill"
	if _, err := s.EnsurePlaythrough(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := s.StartRun(ctx, id, "refusalwar", mekbay.Hangar{URL: "u", Instance: "i"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetForceName(ctx, id, "refusalwar", "Iron Wolves"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddCheckpoint(ctx, id, "refusalwar", "Campaign started", ""); err != nil { // a checkpoint from before the history: history_seq 0
		t.Fatal(err)
	}
	if err := s.LogHistory(ctx, id, "refusalwar", HistoryHangar, "Bought Grizzly for 1,000 SP.", -1000); err != nil {
		t.Fatal(err)
	}
	s.Close()

	for range 2 { // opening again backfills once
		if s, err = OpenSQLite(path); err != nil {
			t.Fatal(err)
		}
		h, err := s.History(ctx, id, "refusalwar")
		if err != nil {
			t.Fatal(err)
		}
		if len(h) != 2 || h[0].Seq != 0 || h[0].Text != "Started the campaign as Iron Wolves." || h[0].Kind != HistoryCampaign || h[0].At.IsZero() || h[0].At.Year() < 2020 {
			t.Fatalf("backfilled start line: %+v", h)
		}
		s.Close()
	}

	// Rolling back to the old checkpoint takes the purchase away but keeps the start.
	if s, err = OpenSQLite(path); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cps, _ := s.Checkpoints(ctx, id, "refusalwar")
	if _, err := s.RestoreCheckpoint(ctx, id, "refusalwar", cps[0].Seq); err != nil {
		t.Fatal(err)
	}
	if h, _ := s.History(ctx, id, "refusalwar"); len(h) != 1 || h[0].Seq != 0 {
		t.Errorf("after the rollback only the start is left: %+v", h)
	}
}
