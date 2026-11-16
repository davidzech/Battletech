package site

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	if pt, ok, _ := s.Playthrough(ctx, id); !ok || pt.Configured() || pt.CanModify() {
		t.Fatalf("new playthrough should exist, unnamed and read-only: %+v", pt)
	}

	// The company name.
	if err := s.SetCompanyName(ctx, id, "Wolf's Dragoons"); err != nil {
		t.Fatal(err)
	}
	if pt, _, _ := s.Playthrough(ctx, id); !pt.Configured() || pt.CompanyName != "Wolf's Dragoons" {
		t.Fatalf("company not saved: %+v", pt)
	}
	if err := s.SetCompanyName(ctx, "no-such-playthrough", "X"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("naming an unknown playthrough should fail with ErrNoRows, got %v", err)
	}

	// Modify mode.
	if err := s.SetOwnerUID(ctx, id, "e66a3544-8fcf-4dab-955a-f4ea2db0b610"); err != nil {
		t.Fatal(err)
	}
	if pt, _, _ := s.Playthrough(ctx, id); !pt.CanModify() {
		t.Fatal("owner UID not saved")
	}
	if err := s.SetOwnerUID(ctx, id, ""); err != nil {
		t.Fatal(err)
	}
	if pt, _, _ := s.Playthrough(ctx, id); pt.CanModify() {
		t.Fatal("empty UID should turn modify mode off")
	}
	if err := s.SetOwnerUID(ctx, "no-such-playthrough", "x"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown playthrough should fail with ErrNoRows, got %v", err)
	}

	// Campaign runs: each campaign has its own started state and hangar.
	if r, ok, err := s.Run(ctx, id, "refusalwar"); err != nil || ok || r.Started() {
		t.Fatalf("nothing started yet: %+v ok=%v err=%v", r, ok, err)
	}
	hangar := Hangar{URL: goodHangarURL, Instance: goodInstance}
	if err := s.StartRun(ctx, id, "refusalwar", hangar); err != nil {
		t.Fatal(err)
	}
	if r, ok, _ := s.Run(ctx, id, "refusalwar"); !ok || !r.Started() || r.Campaign != "refusalwar" || r.HangarInstance != goodInstance || r.HangarURL != goodHangarURL {
		t.Fatalf("run not stored: %+v", r)
	}
	if err := s.StartRun(ctx, id, "another", Hangar{URL: "u2", Instance: "i2"}); err != nil {
		t.Fatal(err)
	}
	if err := s.StartRun(ctx, id, "refusalwar", Hangar{URL: "u3", Instance: "i3"}); err != nil {
		t.Fatalf("starting again should change the hangar: %v", err)
	}
	if r, _, _ := s.Run(ctx, id, "refusalwar"); r.HangarInstance != "i3" || r.HangarURL != "u3" {
		t.Fatalf("hangar not changed: %+v", r)
	}
	if r, _, _ := s.Run(ctx, id, "another"); r.HangarInstance != "i2" {
		t.Fatalf("campaigns must not share a hangar: %+v", r)
	}
	if err := s.StartRun(ctx, "no-such-playthrough", "refusalwar", hangar); err == nil {
		t.Fatal("a run needs a playthrough")
	}

	// Operations are per campaign and scenario.
	if _, ok, err := s.MekBayOperation(ctx, id, "refusalwar", "scenario_000"); err != nil || ok {
		t.Fatalf("no operation yet: ok=%v err=%v", ok, err)
	}
	if err := s.SaveMekBayOperation(ctx, id, "refusalwar", "scenario_000", MekBayOperation{OpForInstance: "f1", OperationID: "o1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMekBayOperation(ctx, id, "refusalwar", "scenario_000", MekBayOperation{OpForInstance: "f2", OperationID: "o2"}); err != nil {
		t.Fatalf("saving again should replace: %v", err)
	}
	if err := s.SaveMekBayOperation(ctx, id, "another", "scenario_000", MekBayOperation{OpForInstance: "f9", OperationID: "o9"}); err != nil {
		t.Fatal(err)
	}
	if op, ok, _ := s.MekBayOperation(ctx, id, "refusalwar", "scenario_000"); !ok || op.OpForInstance != "f2" || op.OperationID != "o2" {
		t.Fatalf("operation not stored: %+v", op)
	}
	if op, _, _ := s.MekBayOperation(ctx, id, "another", "scenario_000"); op.OperationID != "o9" {
		t.Fatalf("two campaigns with a scenario_000 must not share an operation: %+v", op)
	}

	pilotStore(t, s, id)
	portraitStore(t, s, id)
	spStore(t, s, id)
	repairStore(t, s, id)
	rankStore(t, s, id)
	hangarCacheStore(t, s, id)
	abilityStore(t, s, id)
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

// legacyDDL are the tables as they were before companies and per-campaign hangars; withOwner adds the owner_uid
// column that modify mode brought.
func legacyDDL(postgres, withOwner bool) []string {
	ts := "TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP"
	if postgres {
		ts = "TIMESTAMPTZ NOT NULL DEFAULT now()"
	}
	owner := ""
	if withOwner {
		owner = ",\n\towner_uid TEXT NOT NULL DEFAULT ''"
	}
	return []string{
		`CREATE TABLE playthroughs (
	id TEXT PRIMARY KEY,
	created_at ` + ts + `,
	hangar_url TEXT NOT NULL DEFAULT '',
	hangar_instance TEXT NOT NULL DEFAULT '',
	hangar_sel TEXT NOT NULL DEFAULT '',
	players INTEGER NOT NULL DEFAULT 0 CHECK (players BETWEEN 0 AND 4)` + owner + `
)`,
		`CREATE TABLE mekbay_operations (
	playthrough_id TEXT NOT NULL REFERENCES playthroughs(id) ON DELETE CASCADE,
	scenario_id TEXT NOT NULL,
	opfor_instance TEXT NOT NULL,
	operation_id TEXT NOT NULL,
	PRIMARY KEY (playthrough_id, scenario_id)
)`,
	}
}

// seedLegacy fills a database in the old format with two seeds: "seed-a" with a hangar, players, an operation and
// (if withOwner) a MekBay owner UID, and "seed-b" untouched.
func seedLegacy(t *testing.T, db *sql.DB, postgres, withOwner bool) {
	t.Helper()
	stmts := legacyDDL(postgres, withOwner)
	if withOwner {
		stmts = append(stmts, `INSERT INTO playthroughs (id, created_at, hangar_url, hangar_instance, hangar_sel, players, owner_uid) VALUES ('seed-a', '2026-10-12 08:00:00', 'https://mekbay.com/?instance=x', 'x', 'y', 3, 'e66a3544-8fcf-4dab-955a-f4ea2db0b610')`)
	} else {
		stmts = append(stmts, `INSERT INTO playthroughs (id, created_at, hangar_url, hangar_instance, hangar_sel, players) VALUES ('seed-a', '2026-10-12 08:00:00', 'https://mekbay.com/?instance=x', 'x', 'y', 3)`)
	}
	stmts = append(stmts,
		`INSERT INTO playthroughs (id) VALUES ('seed-b')`,
		`INSERT INTO mekbay_operations VALUES ('seed-a', 'scenario_000', 'f', 'o')`)
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%v\n%s", err, stmt)
		}
	}
}

// migrationContract checks that opening a database in the old format (see seedLegacy) keeps its seeds, creation
// times and owner UIDs but drops hangars, player counts and operations, and then works like a new database.
func migrationContract(t *testing.T, s *Store, withOwner bool) {
	t.Helper()
	ctx := context.Background()
	if has, _ := s.columnExists("playthroughs", "hangar_instance"); has {
		t.Fatal("the old hangar columns should be gone")
	}
	if has, _ := s.columnExists("playthroughs", "players"); has {
		t.Fatal("the player count should be gone")
	}
	a, ok, err := s.Playthrough(ctx, "seed-a")
	if err != nil || !ok {
		t.Fatalf("seed-a lost: ok=%v err=%v", ok, err)
	}
	if a.CompanyName != "" || a.Configured() {
		t.Errorf("companies start blank after migrating: %+v", a)
	}
	if withOwner && a.OwnerUID != "e66a3544-8fcf-4dab-955a-f4ea2db0b610" {
		t.Errorf("the owner UID should survive: %+v", a)
	}
	if !withOwner && a.CanModify() {
		t.Errorf("a database from before modify mode has no owner UID: %+v", a)
	}
	if b, ok, _ := s.Playthrough(ctx, "seed-b"); !ok || b.CanModify() {
		t.Errorf("seed-b = %+v, ok=%v", b, ok)
	}
	var created string
	if err := s.db.QueryRow(s.q(`SELECT CAST(created_at AS TEXT) FROM playthroughs WHERE id = ?`), "seed-a").Scan(&created); err != nil || !strings.HasPrefix(created, "2026-10-12 08:00:00") {
		t.Errorf("created_at should be kept, got %q (%v)", created, err)
	}
	var ops int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM mekbay_operations`).Scan(&ops); err != nil || ops != 0 {
		t.Errorf("old operations should be dropped: %d rows, %v", ops, err)
	}
	// The migrated database works like a new one.
	if err := s.SetCompanyName(ctx, "seed-a", "Wolf's Dragoons"); err != nil {
		t.Fatal(err)
	}
	if err := s.StartRun(ctx, "seed-a", "refusalwar", Hangar{URL: goodHangarURL, Instance: goodInstance}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMekBayOperation(ctx, "seed-a", "refusalwar", "scenario_000", MekBayOperation{OpForInstance: "f", OperationID: "o"}); err != nil {
		t.Fatal(err)
	}
	// Deleting a seed takes its runs and operations with it.
	if _, err := s.db.Exec(s.q(`DELETE FROM playthroughs WHERE id = ?`), "seed-a"); err != nil {
		t.Fatal(err)
	}
	var left int
	_ = s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM campaign_runs) + (SELECT COUNT(*) FROM mekbay_operations)`).Scan(&left)
	if left != 0 {
		t.Errorf("runs and operations should cascade with their seed, %d left", left)
	}
}

func TestMigrateLegacySQLite(t *testing.T) {
	for _, withOwner := range []bool{true, false} {
		name := map[bool]string{true: "with owner uid", false: "before modify mode"}[withOwner]
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "old.db")
			db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
			if err != nil {
				t.Fatal(err)
			}
			seedLegacy(t, db, false, withOwner)
			db.Close()

			s, err := OpenSQLite(path)
			if err != nil {
				t.Fatal(err)
			}
			migrationContract(t, s, withOwner)
			s.Close()

			// Opening a migrated database again changes nothing.
			s, err = OpenSQLite(path)
			if err != nil {
				t.Fatalf("reopening a migrated database: %v", err)
			}
			s.Close()
		})
	}
}

// TestMigrateLegacyPostgres runs only when TEST_DATABASE_URL is set. It works in a schema of its own, so the other
// Postgres tests' tables are left alone.
func TestMigrateLegacyPostgres(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	const schema = "legacy_migration_test"
	admin, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`) })

	scoped := url + "?"
	if strings.Contains(url, "?") {
		scoped = url + "&"
	}
	scoped += "search_path=" + schema
	db, err := sql.Open("pgx", scoped)
	if err != nil {
		t.Fatal(err)
	}
	seedLegacy(t, db, true, true)
	db.Close()

	s, err := OpenPostgres(scoped)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	migrationContract(t, s, true)
}
