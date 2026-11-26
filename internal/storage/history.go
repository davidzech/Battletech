package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"modernc.org/sqlite"
)

// The campaign history is a ledger of everything that has happened to a started campaign, oldest first: the start, every purchase, sale, repair, rearm and
// reconfiguration, every change to a pilot, every mission started, aborted or completed, every correction made under Advanced Management. It is append-only and
// is not part of a checkpoint's snapshot, but a rollback takes it back too: the entries made after the checkpoint are deleted, since the campaign returns to that
// point and what came after never happened. The rollback then logs a line of its own (after the deletion), so the history shows that one took place.

// History kinds, which the ledger shows as categories.
const (
	HistoryCampaign = "campaign" // started, renamed, players
	HistoryMission  = "mission"  // battles started and aborted, missions completed
	HistoryPilot    = "pilot"    // recruited, SP, abilities, names, portraits (corrections are HistoryCorrections)
	HistoryHangar   = "hangar"   // bought, sold, repaired, rearmed, reconfigured
	HistoryKeywords = "keywords" // keywords edited by hand
	// HistoryCorrections is every Advanced Management correction (the warchest, a pilot's SP, rank, abilities, killed in action, revived, deleted) and every
	// rollback. It was "funds" when it held only the warchest; Store.init renames those lines.
	HistoryCorrections = "corrections"
)

// HistoryEntry is one line of a campaign's history.
type HistoryEntry struct {
	Seq  int
	At   time.Time
	Kind string
	Text string
	SP   int // the change to the warchest it made (0 for none)
}

// LogHistory appends an entry to a campaign's history. sp is the change it made to the warchest, if any.
func (s *Store) LogHistory(ctx context.Context, playthroughID, campaign, kind, text string, sp int) error {
	var err error
	for range 3 { // the next number is taken in the same statement; two writers racing for it collide on the key, so try again
		_, err = s.db.ExecContext(ctx, s.q(`INSERT INTO campaign_history (playthrough_id, campaign, seq, at, kind, text, sp)
			SELECT ?, ?, COALESCE(MAX(seq), 0) + 1, ?, ?, ?, ? FROM campaign_history WHERE playthrough_id = ? AND campaign = ?`),
			playthroughID, campaign, time.Now().Unix(), kind, text, sp, playthroughID, campaign)
		if err == nil || !keyCollision(err) {
			return err
		}
	}
	return err
}

// SQLite's extended result codes for a row that breaks a primary key or a unique constraint.
const (
	sqliteConstraintPrimaryKey = 1555
	sqliteConstraintUnique     = 2067
)

// keyCollision reports whether a write failed because a row with its key already exists, in either database.
func keyCollision(err error) bool {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code == "23505" // unique_violation
	}
	var lite *sqlite.Error
	if errors.As(err, &lite) {
		return lite.Code() == sqliteConstraintPrimaryKey || lite.Code() == sqliteConstraintUnique
	}
	return false
}

// History returns a campaign's history, oldest first.
func (s *Store) History(ctx context.Context, playthroughID, campaign string) ([]HistoryEntry, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT seq, at, kind, text, sp FROM campaign_history WHERE playthrough_id = ? AND campaign = ? ORDER BY seq`), playthroughID, campaign)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistoryEntry
	for rows.Next() {
		var e HistoryEntry
		var at int64
		if err := rows.Scan(&e.Seq, &at, &e.Kind, &e.Text, &e.SP); err != nil {
			return nil, err
		}
		e.At = time.Unix(at, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}
