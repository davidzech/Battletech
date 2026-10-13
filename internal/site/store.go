package site

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// Store is the SQLite database holding playthrough state.
type Store struct{ db *sql.DB }

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

const schema = `
CREATE TABLE IF NOT EXISTS playthroughs (
	id              TEXT PRIMARY KEY,
	created_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	hangar_url      TEXT NOT NULL DEFAULT '',
	hangar_instance TEXT NOT NULL DEFAULT '',
	hangar_sel      TEXT NOT NULL DEFAULT '',
	players         INTEGER NOT NULL DEFAULT 0 CHECK (players BETWEEN 0 AND 4)
);`

// OpenStore opens (creating if needed) the SQLite database at path and applies the schema.
func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	// Personal-use scale: a single connection avoids "database is locked" surprises.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// EnsurePlaythrough records a playthrough ID if it isn't already known and reports whether it was new.
func (s *Store) EnsurePlaythrough(ctx context.Context, id string) (created bool, err error) {
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO playthroughs (id) VALUES (?)`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Playthrough loads a playthrough; ok is false if the ID is unknown.
func (s *Store) Playthrough(ctx context.Context, id string) (p Playthrough, ok bool, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT id, hangar_url, hangar_instance, hangar_sel, players FROM playthroughs WHERE id = ?`, id,
	).Scan(&p.ID, &p.HangarURL, &p.HangarInstance, &p.HangarSel, &p.Players)
	if errors.Is(err, sql.ErrNoRows) {
		return Playthrough{}, false, nil
	}
	return p, err == nil, err
}

// SaveSetup stores a playthrough's MekBay hangar and human player count.
func (s *Store) SaveSetup(ctx context.Context, id string, h Hangar, players int) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE playthroughs SET hangar_url = ?, hangar_instance = ?, hangar_sel = ?, players = ? WHERE id = ?`,
		h.URL, h.Instance, h.Sel, players, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
