package site

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // pure-Go PostgreSQL driver
	_ "modernc.org/sqlite"             // pure-Go SQLite driver
)

// Store is the database holding playthrough state: a local SQLite file, or PostgreSQL when the site is hosted
// somewhere without a persistent disk. Queries are written with ? placeholders and rebound for PostgreSQL.
type Store struct {
	db       *sql.DB
	postgres bool
}

// Playthrough is one person's tracked run through the campaigns, identified by their seed.
type Playthrough struct {
	ID             string // hash of the seed, never the seed itself
	HangarURL      string // the MekBay hangar link, as the player pasted it
	HangarInstance string // the link's ?instance= id
	HangarSel      string // the link's optional ?sel= id
	Players        int    // number of human players, 1 to 4 (0 until set up)
}

// Configured reports whether the campaign setup (hangar and player count) has been completed.
func (p Playthrough) Configured() bool { return p.HangarInstance != "" && p.Players >= 1 }

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS playthroughs (
	id              TEXT PRIMARY KEY,
	created_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	hangar_url      TEXT NOT NULL DEFAULT '',
	hangar_instance TEXT NOT NULL DEFAULT '',
	hangar_sel      TEXT NOT NULL DEFAULT '',
	players         INTEGER NOT NULL DEFAULT 0 CHECK (players BETWEEN 0 AND 4)
);`

const postgresSchema = `
CREATE TABLE IF NOT EXISTS playthroughs (
	id              TEXT PRIMARY KEY,
	created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
	hangar_url      TEXT NOT NULL DEFAULT '',
	hangar_instance TEXT NOT NULL DEFAULT '',
	hangar_sel      TEXT NOT NULL DEFAULT '',
	players         INTEGER NOT NULL DEFAULT 0 CHECK (players BETWEEN 0 AND 4)
);`

// OpenSQLite opens (creating if needed) the SQLite database at path and applies the schema.
func OpenSQLite(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	// Personal-use scale: a single connection avoids "database is locked" surprises.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(sqliteSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

// OpenPostgres connects to the PostgreSQL database at url (postgres://user:pass@host:port/db?sslmode=require)
// and applies the schema.
func OpenPostgres(url string) (*Store, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(5)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	if _, err := db.Exec(postgresSchema); err != nil {
		diag := postgresDiagnostics(db)
		db.Close()
		return nil, fmt.Errorf("apply schema: %w (%s)", err, diag)
	}
	return &Store{db: db, postgres: true}, nil
}

// postgresDiagnostics describes who we are connected as and what that role may do, to explain a failed schema
// setup (managed Postgres 15+ only lets the database owner create tables in the public schema).
func postgresDiagnostics(db *sql.DB) string {
	var user, database, owner, schemas string
	var inPublic, inDatabase bool
	err := db.QueryRow(`
		SELECT current_user, current_database(),
		       (SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = current_database()),
		       has_schema_privilege('public', 'CREATE'),
		       has_database_privilege(current_database(), 'CREATE'),
		       (SELECT string_agg(nspname || ' owned by ' || pg_get_userbyid(nspowner), ', ') FROM pg_namespace
		         WHERE nspname NOT LIKE 'pg\_%' AND nspname <> 'information_schema')`,
	).Scan(&user, &database, &owner, &inPublic, &inDatabase, &schemas)
	if err != nil {
		return "diagnostics unavailable: " + err.Error()
	}
	return fmt.Sprintf("connected as %q to database %q owned by %q; create in public schema: %v; create schemas: %v; schemas: %s",
		user, database, owner, inPublic, inDatabase, schemas)
}

// q rebinds ? placeholders to $1, $2, ... for PostgreSQL.
func (s *Store) q(query string) string {
	if !s.postgres {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteString("$" + strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// EnsurePlaythrough records a playthrough ID if it isn't already known and reports whether it was new.
func (s *Store) EnsurePlaythrough(ctx context.Context, id string) (created bool, err error) {
	res, err := s.db.ExecContext(ctx, s.q(`INSERT INTO playthroughs (id) VALUES (?) ON CONFLICT (id) DO NOTHING`), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Playthrough loads a playthrough; ok is false if the ID is unknown.
func (s *Store) Playthrough(ctx context.Context, id string) (p Playthrough, ok bool, err error) {
	err = s.db.QueryRowContext(ctx,
		s.q(`SELECT id, hangar_url, hangar_instance, hangar_sel, players FROM playthroughs WHERE id = ?`), id,
	).Scan(&p.ID, &p.HangarURL, &p.HangarInstance, &p.HangarSel, &p.Players)
	if errors.Is(err, sql.ErrNoRows) {
		return Playthrough{}, false, nil
	}
	return p, err == nil, err
}

// SaveSetup stores a playthrough's MekBay hangar and human player count.
func (s *Store) SaveSetup(ctx context.Context, id string, h Hangar, players int) error {
	res, err := s.db.ExecContext(ctx,
		s.q(`UPDATE playthroughs SET hangar_url = ?, hangar_instance = ?, hangar_sel = ?, players = ? WHERE id = ?`),
		h.URL, h.Instance, h.Sel, players, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
