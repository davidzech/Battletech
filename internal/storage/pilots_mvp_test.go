package storage

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/pilot"
)

// A pilot's MVP count: added by a database from before it (every pilot starts at 0), raised by one for each mission committed with them as MVP, and undone by
// a rollback like the rest of the pilot's row.
func TestPilotMVPs(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsurePlaythrough(ctx, "pt"); err != nil {
		t.Fatal(err)
	}
	if err := s.StartRun(ctx, "pt", "camp", []byte(`{"name":"h","groups":[]}`)); err != nil {
		t.Fatal(err)
	}
	p, err := s.AddPilot(ctx, "pt", "camp", "Alrek", "", pilot.SPAllocation{}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`ALTER TABLE named_pilots DROP COLUMN mvps`); err != nil { // a database from before the count
		t.Fatal(err)
	}
	s.Close()
	if s, err = OpenSQLite(path); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mvps := func() int {
		t.Helper()
		ps, err := s.Pilots(ctx, "pt", "camp")
		if err != nil || len(ps) != 1 {
			t.Fatalf("pilots: %v %v", ps, err)
		}
		return ps[0].MVPs
	}
	if n := mvps(); n != 0 {
		t.Fatalf("an existing pilot starts at 0 MVPs, has %d", n)
	}
	if err := s.AddCheckpoint(ctx, "pt", "camp", "Start"); err != nil {
		t.Fatal(err)
	}
	for _, sc := range []string{"s1", "s2"} {
		res := content.OutcomeResult{Pilots: []content.PilotOutcome{{PilotID: p.ID, Name: "Alrek", Status: "active", MVP: true}}}
		if err := s.CompleteWithOutcome(ctx, "pt", "camp", sc, res, nil); err != nil {
			t.Fatal(err)
		}
	}
	// A mission they were not the MVP of leaves the count.
	if err := s.CompleteWithOutcome(ctx, "pt", "camp", "s3", content.OutcomeResult{Pilots: []content.PilotOutcome{{PilotID: p.ID, Name: "Alrek", Status: "active"}}}, nil); err != nil {
		t.Fatal(err)
	}
	if n := mvps(); n != 2 {
		t.Fatalf("two missions as MVP: %d", n)
	}
	cps, err := s.Checkpoints(ctx, "pt", "camp")
	if err != nil || len(cps) != 1 { // only the start: the site takes the others
		t.Fatalf("checkpoints: %v %v", cps, err)
	}
	if err := s.RestoreCheckpoint(ctx, "pt", "camp", cps[0].Seq); err != nil {
		t.Fatal(err)
	}
	if n := mvps(); n != 0 {
		t.Fatalf("a rollback to the start undoes the MVPs: %d", n)
	}
}
