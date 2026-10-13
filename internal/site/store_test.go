package site

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestQRebindsPlaceholders(t *testing.T) {
	pg := &Store{postgres: true}
	if got := pg.q(`UPDATE t SET a = ?, b = ? WHERE id = ?`); got != `UPDATE t SET a = $1, b = $2 WHERE id = $3` {
		t.Errorf("postgres query = %q", got)
	}
	lite := &Store{}
	if got := lite.q(`WHERE id = ?`); got != `WHERE id = ?` {
		t.Errorf("sqlite query = %q", got)
	}
}

// storeContract checks the behaviour both databases must share.
func storeContract(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	id := "store-contract-" + t.Name()
	t.Cleanup(func() { _, _ = s.db.Exec(s.q(`DELETE FROM playthroughs WHERE id = ?`), id) })

	if _, ok, err := s.Playthrough(ctx, id); err != nil || ok {
		t.Fatalf("unknown playthrough: ok=%v err=%v", ok, err)
	}
	if created, err := s.EnsurePlaythrough(ctx, id); err != nil || !created {
		t.Fatalf("first ensure: created=%v err=%v", created, err)
	}
	if created, err := s.EnsurePlaythrough(ctx, id); err != nil || created {
		t.Fatalf("second ensure: created=%v err=%v", created, err)
	}
	if pt, ok, _ := s.Playthrough(ctx, id); !ok || pt.Configured() {
		t.Fatalf("new playthrough should exist and be unconfigured: %+v", pt)
	}
	hangar := Hangar{URL: goodHangar, Instance: goodInstance, Sel: goodSel}
	if err := s.SaveSetup(ctx, id, hangar, 3); err != nil {
		t.Fatal(err)
	}
	pt, _, _ := s.Playthrough(ctx, id)
	if !pt.Configured() || pt.Players != 3 || pt.HangarInstance != goodInstance || pt.HangarSel != goodSel || pt.HangarURL != goodHangar {
		t.Fatalf("setup not saved: %+v", pt)
	}
	if err := s.SaveSetup(ctx, "no-such-playthrough", hangar, 2); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("saving an unknown playthrough should fail with ErrNoRows, got %v", err)
	}
	if err := s.SaveSetup(ctx, id, hangar, 9); err == nil {
		t.Fatal("a player count outside 0 to 4 should be refused by the schema")
	}
}

func TestStoreSQLite(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() }) // registered first, so it runs after the contract's own cleanup
	storeContract(t, s)
}

// TestStorePostgres runs only when TEST_DATABASE_URL points at a throwaway PostgreSQL database.
func TestStorePostgres(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	s, err := OpenPostgres(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() }) // registered first, so it runs after the contract's own cleanup
	storeContract(t, s)
}
