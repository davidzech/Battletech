package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// MaxCheckpoints is how many rollback points a campaign keeps.
const MaxCheckpoints = 20

// snapshotTables are the tables a snapshot holds, with the condition that selects one campaign run's rows (two placeholders: playthrough, campaign), in the order
// they are restored (and the reverse of the order they are cleared).
var snapshotTables = []struct{ table, where string }{
	{"campaign_runs", "playthrough_id = ? AND campaign = ?"},
	{"named_pilots", "playthrough_id = ? AND campaign = ?"},
	{"pilot_abilities", "pilot_id IN (SELECT id FROM named_pilots WHERE playthrough_id = ? AND campaign = ?)"},
	{"scenario_completions", "playthrough_id = ? AND campaign = ?"},
	{"campaign_keywords", "playthrough_id = ? AND campaign = ?"},
	{"mekbay_operations", "playthrough_id = ? AND campaign = ?"},
}

// Checkpoint is one rollback point.
type Checkpoint struct {
	Seq        int
	Label      string
	At         int64  // Unix time
	Snapshot   string // the campaign's rows as JSON, the hangar with the run's row (only loaded by Store.Checkpoint)
	HistorySeq int    // the last history line when it was taken, so the history can offer it on that line, or the newest one it shows before it (0 for a checkpoint older than the history; only loaded by Store.Checkpoints)
	// HasHangar is whether the snapshot holds the hangar. One that doesn't (taken before hangars were stored here, whose hangar couldn't be imported) can't be
	// rolled back to: see RestoreCheckpoint. Only loaded by Store.Checkpoints.
	HasHangar bool
}

// ErrCheckpointNoHangar is RestoreCheckpoint refusing a checkpoint whose snapshot has no hangar.
var ErrCheckpointNoHangar = errors.New("the checkpoint has no hangar")

type snapshotQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// snapshot reads a campaign run's rows from every snapshot table as JSON.
func (s *Store) snapshot(ctx context.Context, q snapshotQuerier, playthroughID, campaign string) (string, error) {
	out := map[string][]map[string]any{}
	for _, t := range snapshotTables {
		rows, err := q.QueryContext(ctx, s.q("SELECT * FROM "+t.table+" WHERE "+t.where), playthroughID, campaign)
		if err != nil {
			return "", err
		}
		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			return "", err
		}
		list := []map[string]any{}
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				return "", err
			}
			row := map[string]any{}
			for i, c := range cols {
				switch v := vals[i].(type) {
				case []byte:
					row[c] = string(v)
				case time.Time:
					row[c] = v.UTC().Format(time.RFC3339Nano)
				default:
					row[c] = v
				}
			}
			list = append(list, row)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return "", err
		}
		rows.Close()
		out[t.table] = list
	}
	b, err := json.Marshal(out)
	return string(b), err
}

// decodeSnapshot reads a snapshot back, with numbers as int64 (or float64 when they have a fraction) so they go into integer columns as they came out.
func decodeSnapshot(raw string) (map[string][]map[string]any, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var out map[string][]map[string]any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	for _, rows := range out {
		for _, row := range rows {
			for k, v := range row {
				if n, ok := v.(json.Number); ok {
					if i, err := n.Int64(); err == nil {
						row[k] = i
					} else if f, err := n.Float64(); err == nil {
						row[k] = f
					}
				}
			}
		}
	}
	return out, nil
}

// AddCheckpoint snapshots the campaign as it is now (its hangar too, which is part of the run's row) and records it as the newest checkpoint, dropping the oldest
// to stay within MaxCheckpoints.
//
// The snapshot and the last history line are read in the one transaction that records the checkpoint, the line first, so the two agree: every change is made
// before its line is logged, so a change whose line the checkpoint keeps is in its snapshot, and the snapshot's tables are all read as of one moment (SQLite has
// one connection, which the transaction holds; PostgreSQL is asked for a transaction that reads one snapshot of the database throughout).
func (s *Store) AddCheckpoint(ctx context.Context, playthroughID, campaign, label string) error {
	var opts *sql.TxOptions
	if s.postgres {
		opts = &sql.TxOptions{Isolation: sql.LevelRepeatableRead}
	}
	tx, err := s.db.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var historySeq int
	if err := tx.QueryRowContext(ctx, s.q(`SELECT COALESCE(MAX(seq), 0) FROM campaign_history WHERE playthrough_id = ? AND campaign = ?`), playthroughID, campaign).Scan(&historySeq); err != nil {
		return err
	}
	snap, err := s.snapshot(ctx, tx, playthroughID, campaign)
	if err != nil {
		return err
	}
	var seq int
	if err := tx.QueryRowContext(ctx, s.q(`SELECT COALESCE(MAX(seq), 0) + 1 FROM campaign_checkpoints WHERE playthrough_id = ? AND campaign = ?`), playthroughID, campaign).Scan(&seq); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO campaign_checkpoints (playthrough_id, campaign, seq, label, created_at, snapshot, history_seq) VALUES (?, ?, ?, ?, ?, ?, ?)`),
		playthroughID, campaign, seq, label, time.Now().Unix(), snap, historySeq); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, s.q(`SELECT seq FROM campaign_checkpoints WHERE playthrough_id = ? AND campaign = ? ORDER BY seq DESC`), playthroughID, campaign)
	if err != nil {
		return err
	}
	var old []int
	n := 0
	for rows.Next() {
		var q int
		if err := rows.Scan(&q); err != nil {
			rows.Close()
			return err
		}
		if n++; n > MaxCheckpoints {
			old = append(old, q)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, q := range old {
		if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM campaign_checkpoints WHERE playthrough_id = ? AND campaign = ? AND seq = ?`), playthroughID, campaign, q); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Checkpoints lists a campaign's rollback points, newest first (without their snapshots).
func (s *Store) Checkpoints(ctx context.Context, playthroughID, campaign string) ([]Checkpoint, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT seq, label, created_at, history_seq, CASE WHEN snapshot LIKE ? THEN 1 ELSE 0 END FROM campaign_checkpoints
		WHERE playthrough_id = ? AND campaign = ? ORDER BY seq DESC`), snapshotWithHangar, playthroughID, campaign)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Checkpoint
	for rows.Next() {
		var c Checkpoint
		var has int
		if err := rows.Scan(&c.Seq, &c.Label, &c.At, &c.HistorySeq, &has); err != nil {
			return nil, err
		}
		c.HasHangar = has == 1
		out = append(out, c)
	}
	return out, rows.Err()
}

// Checkpoint loads one rollback point with its snapshot (ok is false if there is none).
func (s *Store) Checkpoint(ctx context.Context, playthroughID, campaign string, seq int) (c Checkpoint, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, s.q(`SELECT seq, label, created_at, snapshot FROM campaign_checkpoints WHERE playthrough_id = ? AND campaign = ? AND seq = ?`), playthroughID, campaign, seq).
		Scan(&c.Seq, &c.Label, &c.At, &c.Snapshot)
	if errors.Is(err, sql.ErrNoRows) {
		return Checkpoint{}, false, nil
	}
	return c, err == nil, err
}

// RestoreCheckpoint puts the campaign's rows back as they were in the checkpoint (the hangar with the run's row) and forgets every later checkpoint, all in one
// transaction. A checkpoint with no hangar of its own (taken before hangars were stored here, and whose hangar couldn't be imported) is refused with
// ErrCheckpointNoHangar: restoring the rest (the warchest, the pilots) around today's units would undo payments without undoing what they bought.
func (s *Store) RestoreCheckpoint(ctx context.Context, playthroughID, campaign string, seq int) error {
	cp, ok, err := s.Checkpoint(ctx, playthroughID, campaign, seq)
	if err != nil {
		return err
	}
	if !ok {
		return sql.ErrNoRows
	}
	snap, err := decodeSnapshot(cp.Snapshot)
	if err != nil {
		return fmt.Errorf("the checkpoint could not be read: %w", err)
	}
	if len(snap["campaign_runs"]) == 0 {
		return ErrCheckpointNoHangar
	}
	if h, _ := snap["campaign_runs"][0]["hangar"].(string); h == "" {
		return ErrCheckpointNoHangar
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.lockRun(ctx, tx, playthroughID, campaign); err != nil { // before the pilots' rows, as everywhere
		return err
	}
	for i := len(snapshotTables) - 1; i >= 0; i-- {
		t := snapshotTables[i]
		if _, err := tx.ExecContext(ctx, s.q("DELETE FROM "+t.table+" WHERE "+t.where), playthroughID, campaign); err != nil {
			return err
		}
	}
	for _, t := range snapshotTables {
		for _, row := range snap[t.table] {
			cols := make([]string, 0, len(row))
			for k := range row {
				cols = append(cols, k)
			}
			sort.Strings(cols)
			args := make([]any, len(cols))
			marks := make([]string, len(cols))
			for i, k := range cols {
				args[i], marks[i] = row[k], "?"
			}
			if _, err := tx.ExecContext(ctx, s.q("INSERT INTO "+t.table+" ("+strings.Join(cols, ", ")+") VALUES ("+strings.Join(marks, ", ")+")"), args...); err != nil {
				return fmt.Errorf("restoring %s: %w", t.table, err)
			}
		}
	}
	// The history goes back with the rest: what happened after the checkpoint never happened.
	if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM campaign_history WHERE playthrough_id = ? AND campaign = ?
		AND seq > (SELECT history_seq FROM campaign_checkpoints WHERE playthrough_id = ? AND campaign = ? AND seq = ?)`), playthroughID, campaign, playthroughID, campaign, seq); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM campaign_checkpoints WHERE playthrough_id = ? AND campaign = ? AND seq > ?`), playthroughID, campaign, seq); err != nil {
		return err
	}
	return tx.Commit()
}
