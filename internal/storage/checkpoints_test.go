package storage

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

// checkpointRace takes checkpoints while another request keeps changing the campaign and logging each change, as every endpoint does: the change first, then
// its history line. A checkpoint must hold every change whose line it keeps, or a rollback would keep the line ("warchest set to 7") and lose the change.
func checkpointRace(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	id := "checkpoint-race-" + t.Name()
	t.Cleanup(func() { _, _ = s.db.Exec(s.q(`DELETE FROM playthroughs WHERE id = ?`), id) })
	if _, err := s.EnsurePlaythrough(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := s.StartRun(ctx, id, "camp", testHangar(goodInstance)); err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // the other request: set the warchest to n, then log a line that says n
		defer wg.Done()
		for n := 1; ; n++ {
			select {
			case <-stop:
				return
			default:
			}
			if err := s.SetWarchest(ctx, id, "camp", n); err != nil {
				t.Error(err)
				return
			}
			if err := s.LogHistory(ctx, id, "camp", HistoryCorrections, strconv.Itoa(n), 0); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	defer func() {
		close(stop)
		wg.Wait()
	}()

	for i := 0; i < 300; i++ {
		if err := s.AddCheckpoint(ctx, id, "camp", "Checkpoint "+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
		cps, err := s.Checkpoints(ctx, id, "camp")
		if err != nil || len(cps) == 0 {
			t.Fatal(cps, err)
		}
		cp, ok, err := s.Checkpoint(ctx, id, "camp", cps[0].Seq)
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
		if cps[0].HistorySeq == 0 {
			continue // nothing logged yet
		}
		var line string
		if err := s.db.QueryRowContext(ctx, s.q(`SELECT text FROM campaign_history WHERE playthrough_id = ? AND campaign = ? AND seq = ?`), id, "camp", cps[0].HistorySeq).Scan(&line); err != nil {
			t.Fatal(err)
		}
		logged, err := strconv.Atoi(line)
		if err != nil {
			t.Fatal(err)
		}
		snap, err := decodeSnapshot(cp.Snapshot)
		if err != nil || len(snap["campaign_runs"]) != 1 {
			t.Fatal(snap, err)
		}
		held, _ := snap["campaign_runs"][0]["warchest"].(int64)
		if int(held) < logged {
			t.Fatalf("checkpoint %d keeps the line that set the warchest to %d, but its snapshot holds %d", i, logged, held)
		}
	}
}

func TestCheckpointHoldsWhatItsHistoryKeepsSQLite(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	checkpointRace(t, s)
}

// TestCheckpointHoldsWhatItsHistoryKeepsPostgres runs only when TEST_DATABASE_URL points at a throwaway PostgreSQL database.
func TestCheckpointHoldsWhatItsHistoryKeepsPostgres(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	s, err := OpenPostgres(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	checkpointRace(t, s)
}
