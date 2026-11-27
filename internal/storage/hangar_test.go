package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/content"
)

// unitCount is how many units a stored hangar's first group holds (hangars in these tests have one group, "units": [...]).
func unitCount(t *testing.T, s *Store, ctx context.Context, playthroughID, campaign string) int {
	t.Helper()
	raw, err := s.Hangar(ctx, playthroughID, campaign)
	if err != nil {
		t.Fatal(err)
	}
	var h struct {
		Groups []struct {
			Units []any `json:"units"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(raw, &h); err != nil || len(h.Groups) != 1 {
		t.Fatalf("hangar %s: %v", raw, err)
	}
	return len(h.Groups[0].Units)
}

// addUnit is a hangar edit that adds a unit to the first group and charges cost SP.
func addUnit(cost int) HangarEdit {
	return func(h map[string]any) (int, error) {
		g := h["groups"].([]any)[0].(map[string]any)
		g["units"] = append(g["units"].([]any), map[string]any{"id": "x", "unit": "BMGrizzly"})
		return -cost, nil
	}
}

const oneGroup = `{"name":"H","groups":[{"name":"Hangar","units":[]}]}`

func hangarStore(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	id := "hangar-" + t.Name()
	t.Cleanup(func() { _, _ = s.db.Exec(s.q(`DELETE FROM playthroughs WHERE id = ?`), id) })
	if _, err := s.EnsurePlaythrough(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Hangar(ctx, id, "camp"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("a campaign not started has no hangar: %v", err)
	}
	if _, err := s.EditHangar(ctx, id, "camp", addUnit(0)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("nor can one be edited: %v", err)
	}
	if err := s.StartRunWithPilots(ctx, id, "camp", []byte("not json"), nil, 100, 1, "F"); err == nil {
		t.Fatal("a hangar must be JSON")
	}
	if err := s.StartRunWithPilots(ctx, id, "camp", []byte(oneGroup), nil, 100, 1, "F"); err != nil {
		t.Fatal(err)
	}

	// A paid edit changes the hangar and the warchest together.
	if left, err := s.EditHangar(ctx, id, "camp", addUnit(60)); err != nil || left != 40 {
		t.Fatalf("a paid edit: %d %v", left, err)
	}
	if n := unitCount(t, s, ctx, id, "camp"); n != 1 {
		t.Fatalf("the edit is saved: %d units", n)
	}
	// One the warchest can't cover changes neither, and says what there is.
	if left, err := s.EditHangar(ctx, id, "camp", addUnit(41)); !errors.Is(err, ErrInsufficientFunds) || left != 40 {
		t.Fatalf("too dear: %d %v", left, err)
	}
	// An edit that fails changes nothing either.
	boom := errors.New("boom")
	if _, err := s.EditHangar(ctx, id, "camp", func(h map[string]any) (int, error) { h["groups"] = []any{}; return 0, boom }); !errors.Is(err, boom) {
		t.Fatalf("a failed edit: %v", err)
	}
	if r, _, _ := s.Run(ctx, id, "camp"); unitCount(t, s, ctx, id, "camp") != 1 || r.Warchest != 40 {
		t.Fatalf("nothing changed: %+v", r)
	}
	// A sale pays into the warchest.
	if left, err := s.EditHangar(ctx, id, "camp", func(h map[string]any) (int, error) {
		h["groups"].([]any)[0].(map[string]any)["units"] = []any{}
		return 25, nil
	}); err != nil || left != 65 || unitCount(t, s, ctx, id, "camp") != 0 {
		t.Fatalf("a sale: %d %v", left, err)
	}

	// Completing a mission can write the battle back into the hangar in the same transaction; a write-back that fails completes nothing.
	if err := s.CompleteWithOutcome(ctx, id, "camp", "s1", content.OutcomeResult{Warchest: 10}, func(map[string]any) (int, error) { return 0, boom }); !errors.Is(err, boom) {
		t.Fatalf("a failed write-back: %v", err)
	}
	if done, _ := s.Completions(ctx, id, "camp"); done["s1"] {
		t.Fatal("a failed write-back leaves the mission open")
	}
	if err := s.CompleteWithOutcome(ctx, id, "camp", "s1", content.OutcomeResult{Warchest: 10}, addUnit(0)); err != nil {
		t.Fatal(err)
	}
	if r, _, _ := s.Run(ctx, id, "camp"); unitCount(t, s, ctx, id, "camp") != 1 || r.Warchest != 75 {
		t.Fatalf("the write-back and the outcome: %+v", r)
	}

	// Edits made at once all count: each starts from the hangar the last one saved.
	errs := atOnce(4, func(int) error { _, err := s.EditHangar(ctx, id, "camp", addUnit(1)); return err })
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if r, _, _ := s.Run(ctx, id, "camp"); unitCount(t, s, ctx, id, "camp") != 5 || r.Warchest != 71 {
		t.Fatalf("four edits at once: %d units, %+v", unitCount(t, s, ctx, id, "camp"), r)
	}

	// A checkpoint holds the hangar, and rolling back to it restores it.
	if err := s.AddCheckpoint(ctx, id, "camp", "Before"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EditHangar(ctx, id, "camp", addUnit(0)); err != nil {
		t.Fatal(err)
	}
	cps, _ := s.Checkpoints(ctx, id, "camp")
	if err := s.RestoreCheckpoint(ctx, id, "camp", cps[0].Seq); err != nil {
		t.Fatal(err)
	}
	if n := unitCount(t, s, ctx, id, "camp"); n != 5 {
		t.Fatalf("the rollback restores the hangar: %d units", n)
	}
}

func TestHangarSQLite(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	hangarStore(t, s)
}

// TestHangarPostgres runs only when TEST_DATABASE_URL points at a throwaway PostgreSQL database, where edits made at once really overlap.
func TestHangarPostgres(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	s, err := OpenPostgres(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	hangarStore(t, s)
}

// A database from before hangars were stored here: runs and checkpoints point at forces in MekBay, and the hangar cache table exists. Opening it adds the hangar
// column and drops the cache; the import lists what is still in MekBay until it is stored, and a checkpoint without a hangar keeps the live one on a rollback.
func TestHangarMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	// Turn the new database back into an old one: no hangar column, a cache table, runs and checkpoints in MekBay.
	for _, stmt := range []string{
		`ALTER TABLE campaign_runs DROP COLUMN hangar`,
		`CREATE TABLE hangar_cache (playthrough_id TEXT NOT NULL, campaign TEXT NOT NULL, instance TEXT NOT NULL, data TEXT NOT NULL, fetched_at BIGINT NOT NULL, PRIMARY KEY (playthrough_id, campaign))`,
		`INSERT INTO playthroughs (id) VALUES ('pt')`,
		`INSERT INTO campaign_runs (playthrough_id, campaign, hangar_url, hangar_instance, source_url) VALUES ('pt', 'owned', 'u', 'live-1', 'src')`,
		`INSERT INTO campaign_runs (playthrough_id, campaign, hangar_url, hangar_instance) VALUES ('pt', 'linked', 'u', 'players-own')`,
		`INSERT INTO hangar_cache VALUES ('pt', 'owned', 'live-1', '{}', 0)`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			t.Fatal(stmt, err)
		}
	}
	// Checkpoints taken by the old site: their snapshots have no hangar.
	if err := s.AddCheckpoint(ctx, "pt", "owned", "Start"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddCheckpoint(ctx, "pt", "linked", "Start"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE campaign_checkpoints SET hangar_copy = 'frozen-1' WHERE campaign = 'owned'`); err != nil {
		t.Fatal(err)
	}
	s.Close()

	if s, err = OpenSQLite(path); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if has, err := s.columnExists("campaign_runs", "hangar"); err != nil || !has {
		t.Fatalf("the hangar column is added: %v %v", has, err)
	}
	if has, _ := s.columnExists("hangar_cache", "data"); has {
		t.Error("the hangar cache is dropped")
	}
	runs, err := s.RunHangarImports(ctx)
	if err != nil || len(runs) != 2 || runs[0].Campaign != "linked" || runs[0].Instance != "players-own" || runs[1].Instance != "live-1" {
		t.Fatalf("runs to import: %+v %v", runs, err)
	}
	cps, err := s.CheckpointHangarImports(ctx)
	if err != nil || len(cps) != 1 || cps[0].Campaign != "owned" || cps[0].Instance != "frozen-1" || cps[0].Seq != 1 {
		t.Fatalf("checkpoints to import: %+v %v", cps, err)
	}

	// Importing stores them, and they are listed no more.
	if err := s.SetHangar(ctx, "pt", "owned", []byte(`{"name":"live"}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetHangar(ctx, "pt", "linked", []byte(`{"name":"linked"}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCheckpointHangar(ctx, "pt", "owned", 1, []byte(`{"name":"frozen"}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCheckpointHangar(ctx, "pt", "owned", 9, []byte(`{}`)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("no such checkpoint: %v", err)
	}
	if runs, _ := s.RunHangarImports(ctx); len(runs) != 0 {
		t.Errorf("imported runs are not listed again: %+v", runs)
	}
	if cps, _ := s.CheckpointHangarImports(ctx); len(cps) != 0 {
		t.Errorf("imported checkpoints are not listed again: %+v", cps)
	}

	// Rolling back to the imported checkpoint restores its hangar; one with no hangar (a run whose hangar the site never kept) keeps the live one.
	if err := s.RestoreCheckpoint(ctx, "pt", "owned", 1); err != nil {
		t.Fatal(err)
	}
	if h, _ := s.Hangar(ctx, "pt", "owned"); string(h) != `{"name":"frozen"}` {
		t.Errorf("the checkpoint's hangar: %s", h)
	}
	if err := s.RestoreCheckpoint(ctx, "pt", "linked", 1); err != nil {
		t.Fatal(err)
	}
	if h, _ := s.Hangar(ctx, "pt", "linked"); string(h) != `{"name":"linked"}` {
		t.Errorf("a checkpoint without a hangar keeps the live one: %s", h)
	}
	if r, ok, _ := s.Run(ctx, "pt", "linked"); !ok || !r.Started() {
		t.Errorf("the run is still started: %+v", r)
	}
	if err := s.SetHangar(ctx, "pt", "nope", []byte(`{}`)); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("no such run: %v", err)
	}
	if err := s.SetHangar(ctx, "pt", "owned", []byte(`{`)); err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Errorf("not JSON: %v", err)
	}
}
