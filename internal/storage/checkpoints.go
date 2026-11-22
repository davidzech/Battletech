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
	HangarCopy string // the frozen hangar force's instance ID in MekBay ("" if the run's hangar isn't one the site owns)
	Snapshot   string // the campaign's rows as JSON (only loaded by Store.Checkpoint)
}

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

// SnapshotHangarInstance is the hangar instance the snapshot's run row points at ("" if it has none).
func SnapshotHangarInstance(raw string) string {
	snap, err := decodeSnapshot(raw)
	if err != nil || len(snap["campaign_runs"]) == 0 {
		return ""
	}
	inst, _ := snap["campaign_runs"][0]["hangar_instance"].(string)
	return inst
}

// AddCheckpoint snapshots the campaign as it is now and records it as the newest checkpoint, with the frozen hangar copy (which may be empty). It returns the
// frozen hangars of the oldest checkpoints dropped to stay within maxCheckpoints, for the caller to delete in MekBay.
func (s *Store) AddCheckpoint(ctx context.Context, playthroughID, campaign, label, hangarCopy string) (dropped []string, err error) {
	snap, err := s.snapshot(ctx, s.db, playthroughID, campaign)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var seq int
	if err := tx.QueryRowContext(ctx, s.q(`SELECT COALESCE(MAX(seq), 0) + 1 FROM campaign_checkpoints WHERE playthrough_id = ? AND campaign = ?`), playthroughID, campaign).Scan(&seq); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO campaign_checkpoints (playthrough_id, campaign, seq, label, created_at, hangar_copy, snapshot) VALUES (?, ?, ?, ?, ?, ?, ?)`),
		playthroughID, campaign, seq, label, time.Now().Unix(), hangarCopy, snap); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, s.q(`SELECT seq, hangar_copy FROM campaign_checkpoints WHERE playthrough_id = ? AND campaign = ? ORDER BY seq DESC`), playthroughID, campaign)
	if err != nil {
		return nil, err
	}
	var old []int
	n := 0
	for rows.Next() {
		var q int
		var h string
		if err := rows.Scan(&q, &h); err != nil {
			rows.Close()
			return nil, err
		}
		if n++; n > MaxCheckpoints {
			old = append(old, q)
			if h != "" {
				dropped = append(dropped, h)
			}
		}
	}
	rows.Close()
	for _, q := range old {
		if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM campaign_checkpoints WHERE playthrough_id = ? AND campaign = ? AND seq = ?`), playthroughID, campaign, q); err != nil {
			return nil, err
		}
	}
	return dropped, tx.Commit()
}

// Checkpoints lists a campaign's rollback points, newest first (without their snapshots).
func (s *Store) Checkpoints(ctx context.Context, playthroughID, campaign string) ([]Checkpoint, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT seq, label, created_at, hangar_copy FROM campaign_checkpoints WHERE playthrough_id = ? AND campaign = ? ORDER BY seq DESC`), playthroughID, campaign)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Checkpoint
	for rows.Next() {
		var c Checkpoint
		if err := rows.Scan(&c.Seq, &c.Label, &c.At, &c.HangarCopy); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Checkpoint loads one rollback point with its snapshot (ok is false if there is none).
func (s *Store) Checkpoint(ctx context.Context, playthroughID, campaign string, seq int) (c Checkpoint, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, s.q(`SELECT seq, label, created_at, hangar_copy, snapshot FROM campaign_checkpoints WHERE playthrough_id = ? AND campaign = ? AND seq = ?`), playthroughID, campaign, seq).
		Scan(&c.Seq, &c.Label, &c.At, &c.HangarCopy, &c.Snapshot)
	if errors.Is(err, sql.ErrNoRows) {
		return Checkpoint{}, false, nil
	}
	return c, err == nil, err
}

// RestoreCheckpoint puts the campaign's rows back as they were in the checkpoint and forgets every later checkpoint, all in one transaction. It returns the frozen
// hangars of the forgotten checkpoints, for the caller to delete in MekBay. The hangar cache and the battle status are cleared with the rest.
func (s *Store) RestoreCheckpoint(ctx context.Context, playthroughID, campaign string, seq int) (dropped []string, err error) {
	cp, ok, err := s.Checkpoint(ctx, playthroughID, campaign, seq)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, sql.ErrNoRows
	}
	snap, err := decodeSnapshot(cp.Snapshot)
	if err != nil {
		return nil, fmt.Errorf("the checkpoint could not be read: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for i := len(snapshotTables) - 1; i >= 0; i-- {
		t := snapshotTables[i]
		if _, err := tx.ExecContext(ctx, s.q("DELETE FROM "+t.table+" WHERE "+t.where), playthroughID, campaign); err != nil {
			return nil, err
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
				return nil, fmt.Errorf("restoring %s: %w", t.table, err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM hangar_cache WHERE playthrough_id = ? AND campaign = ?`), playthroughID, campaign); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, s.q(`SELECT hangar_copy FROM campaign_checkpoints WHERE playthrough_id = ? AND campaign = ? AND seq > ?`), playthroughID, campaign, seq)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			rows.Close()
			return nil, err
		}
		if h != "" {
			dropped = append(dropped, h)
		}
	}
	rows.Close()
	if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM campaign_checkpoints WHERE playthrough_id = ? AND campaign = ? AND seq > ?`), playthroughID, campaign, seq); err != nil {
		return nil, err
	}
	return dropped, tx.Commit()
}

// CheckpointHangars lists every frozen hangar a campaign's checkpoints hold (to delete them when the campaign is).
func (s *Store) CheckpointHangars(ctx context.Context, playthroughID, campaign string) ([]string, error) {
	cps, err := s.Checkpoints(ctx, playthroughID, campaign)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, c := range cps {
		if c.HangarCopy != "" {
			out = append(out, c.HangarCopy)
		}
	}
	return out, nil
}
