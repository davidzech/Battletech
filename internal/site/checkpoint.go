package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/davidzech/Battletech/internal/mekbay"
)

// Checkpoints: the campaign's rollback points.
//
// Everything the player does between missions (repairs, sales, purchases, pilots' SP) changes the live campaign in place, as ever. A checkpoint is taken when the
// campaign starts and right after each mission is completed: a snapshot of the campaign's rows in the database, and a frozen copy of the hangar force in MekBay (a
// force of its own that nothing touches). Rolling back to one restores the rows and copies the frozen hangar back over the live one, so the campaign is exactly as
// it was at that moment. Aborting a mission rolls back to the latest checkpoint, which is the end of the previous mission. A campaign keeps at most
// maxCheckpoints; the oldest are dropped and their frozen hangars deleted, as are the frozen hangars of every checkpoint after one rolled back to.

// maxCheckpoints is how many rollback points a campaign keeps.
const maxCheckpoints = 20

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

// snapshotHangarInstance is the hangar instance the snapshot's run row points at ("" if it has none).
func snapshotHangarInstance(raw string) string {
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
		if n++; n > maxCheckpoints {
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

// ---- MekBay: frozen copies

// ---- the server

// checkpointCopy makes the frozen hangar copy a new checkpoint will hold: a copy of the run's live hangar as it is now. It returns "" for a run whose hangar the
// site doesn't own (nothing to freeze).
func (s *Server) checkpointCopy(ctx context.Context, seed string, run CampaignRun) (string, error) {
	if !run.Owned() {
		return "", nil
	}
	id := mekbay.NewID()
	if err := s.mekbay.CopyForce(ctx, mekbay.CopyRequest{OwnerUID: mekbay.AccountUID(seed, mekbay.SlotHangar), Source: run.HangarInstance, Target: id}); err != nil {
		return "", err
	}
	return id, nil
}

// deleteHangars deletes forces the site made in MekBay for a seed, in the background of the request (a failure only leaves unused forces behind).
func (s *Server) deleteHangars(ctx context.Context, seed string, instances []string) {
	if len(instances) == 0 {
		return
	}
	if err := s.mekbay.DeleteForce(context.WithoutCancel(ctx), mekbay.AccountUID(seed, mekbay.SlotHangar), instances...); err != nil {
		log.Printf("delete %d unused forces in MekBay: %v", len(instances), err)
	}
}

// recordCheckpoint snapshots the campaign as it is now as a new checkpoint holding the frozen hangar copy; the frozen hangars of checkpoints dropped for age are
// deleted. If it can't be recorded the copy is deleted again.
func (s *Server) recordCheckpoint(ctx context.Context, seed, playthroughID, campaign, label, copyID string) {
	dropped, err := s.store.AddCheckpoint(ctx, playthroughID, campaign, label, copyID)
	if err != nil {
		log.Printf("record a checkpoint (%s): %v", label, err)
		if copyID != "" {
			s.deleteHangars(ctx, seed, []string{copyID})
		}
		return
	}
	s.deleteHangars(ctx, seed, dropped)
}

// rollbackTo puts the campaign back as it was at a checkpoint: the frozen hangar is copied over the live one first (if that fails nothing has changed), then the
// database rows are restored and the later checkpoints forgotten, and their frozen hangars deleted.
func (s *Server) rollbackTo(ctx context.Context, seed, playthroughID, campaign string, seq int) error {
	cp, ok, err := s.store.Checkpoint(ctx, playthroughID, campaign, seq)
	if err != nil {
		return err
	}
	if !ok {
		return sql.ErrNoRows
	}
	cur, _, err := s.store.Run(ctx, playthroughID, campaign)
	if err != nil {
		return err
	}
	live := snapshotHangarInstance(cp.Snapshot)
	if cp.HangarCopy != "" && live != "" {
		if err := s.mekbay.CopyForce(ctx, mekbay.CopyRequest{OwnerUID: mekbay.AccountUID(seed, mekbay.SlotHangar), Source: cp.HangarCopy, Target: live}); err != nil {
			return fmt.Errorf("could not restore the hangar in MekBay: %w", err)
		}
	}
	dropped, err := s.store.RestoreCheckpoint(ctx, playthroughID, campaign, seq)
	if err != nil {
		return err
	}
	if cur.Owned() && cur.HangarInstance != live && cur.HangarInstance != "" {
		dropped = append(dropped, cur.HangarInstance) // a hangar imported since is not part of the campaign any more
	}
	s.deleteHangars(ctx, seed, dropped)
	return nil
}

// rollbackCampaign is POST /api/campaign/rollback: restore the campaign to one of its checkpoints.
func (s *Server) rollbackCampaign(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	seq, err := strconv.Atoi(r.PostFormValue("checkpoint"))
	if err != nil {
		http.Error(w, "Choose a checkpoint.", http.StatusBadRequest)
		return
	}
	switch err := s.rollbackTo(r.Context(), currentSeed(r), pt.ID, c.Slug, seq); {
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "That checkpoint no longer exists.", http.StatusNotFound)
	case err != nil:
		log.Printf("roll back: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// CheckpointView is a checkpoint as the Manage page lists it.
type CheckpointView struct {
	Seq    int
	Label  string
	When   string
	Latest bool
}

func checkpointViews(cps []Checkpoint) []CheckpointView {
	out := make([]CheckpointView, 0, len(cps))
	for i, c := range cps {
		out = append(out, CheckpointView{Seq: c.Seq, Label: c.Label, When: time.Unix(c.At, 0).UTC().Format("2006-01-02 15:04 UTC"), Latest: i == 0})
	}
	return out
}
