package storage

import (
	"context"
	"time"
)

// The campaign history is a ledger of everything that has happened to a started campaign, oldest first: the start, every purchase, sale, repair, rearm and
// reconfiguration, every change to a pilot, every mission started, aborted or completed, every correction made under Advanced Management. It is append-only and
// is not part of a checkpoint's snapshot, but a rollback takes it back too: the entries made after the checkpoint are deleted, since the campaign returns to that
// point and what came after never happened (the rollback itself is not logged).

// History kinds, which the ledger shows as categories.
const (
	HistoryCampaign = "campaign" // started, renamed, players
	HistoryMission  = "mission"  // battles started and aborted, missions completed
	HistoryPilot    = "pilot"    // recruited, SP, abilities, killed, ranks, names, portraits
	HistoryHangar   = "hangar"   // bought, sold, repaired, rearmed, reconfigured
	HistoryFunds    = "funds"    // the warchest set by hand
	HistoryKeywords = "keywords" // keywords edited by hand
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
		if err == nil {
			return nil
		}
	}
	return err
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
