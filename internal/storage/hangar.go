package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// A campaign run's hangar lives in its row of campaign_runs (column hangar): the force as MekBay saves one (the JSON of a force's `data`), kept as it is, so the
// edits in package mekbay (repairs, sales, swaps, purchases, a battle's write-back) run on it unchanged and a battle's forces are copied out of it. Being a column of
// the run's row, it is part of every checkpoint's snapshot, and a rollback restores it with the rest.

// ErrNoHangar is returned when a campaign run has no hangar stored.
var ErrNoHangar = errors.New("this campaign has no hangar stored")

// Hangar loads a campaign run's hangar (sql.ErrNoRows if the campaign isn't started, ErrNoHangar if it has none).
func (s *Store) Hangar(ctx context.Context, playthroughID, campaign string) ([]byte, error) {
	var data string
	if err := s.db.QueryRowContext(ctx, s.q(`SELECT hangar FROM campaign_runs WHERE playthrough_id = ? AND campaign = ?`), playthroughID, campaign).Scan(&data); err != nil {
		return nil, err
	}
	if data == "" {
		return nil, ErrNoHangar
	}
	return []byte(data), nil
}

// SetHangar replaces a campaign run's hangar (sql.ErrNoRows if the campaign isn't started). It is for importing one; the site changes a hangar with EditHangar.
func (s *Store) SetHangar(ctx context.Context, playthroughID, campaign string, data []byte) error {
	if !json.Valid(data) {
		return errors.New("the hangar is not valid JSON")
	}
	res, err := s.db.ExecContext(ctx, s.q(`UPDATE campaign_runs SET hangar = ? WHERE playthrough_id = ? AND campaign = ?`), string(data), playthroughID, campaign)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// HangarEdit changes a hangar (decoded from its JSON) in place and returns the SP it moves into the warchest: negative for a charge, positive for a sale's price.
// An error leaves the hangar and the warchest as they were.
type HangarEdit func(hangar map[string]any) (sp int, err error)

// EditHangar changes a campaign run's hangar and its warchest together, in one transaction, and returns the warchest it left. The transaction takes the run's
// row lock first (lockRun), so edits made at once, on one server or several sharing the database, take turns and each starts from the hangar the last one saved.
// A charge the warchest can't cover changes nothing and returns ErrInsufficientFunds with the warchest as it is; an error from edit changes nothing and is
// returned as it is. sql.ErrNoRows if the campaign isn't started, ErrNoHangar if it has no hangar.
func (s *Store) EditHangar(ctx context.Context, playthroughID, campaign string, edit HangarEdit) (warchest int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := s.lockRun(ctx, tx, playthroughID, campaign); err != nil {
		return 0, err
	}
	var data string
	if err := tx.QueryRowContext(ctx, s.q(`SELECT hangar, warchest FROM campaign_runs WHERE playthrough_id = ? AND campaign = ?`), playthroughID, campaign).Scan(&data, &warchest); err != nil {
		return 0, err
	}
	hangar, err := decodeHangar(data)
	if err != nil {
		return warchest, err
	}
	sp, err := edit(hangar)
	if err != nil {
		return warchest, err
	}
	if warchest+sp < 0 {
		return warchest, ErrInsufficientFunds
	}
	if err := s.saveHangar(ctx, tx, playthroughID, campaign, hangar, sp); err != nil {
		return warchest, err
	}
	return warchest + sp, tx.Commit()
}

// decodeHangar reads a stored hangar into the form the edits in package mekbay take.
func decodeHangar(data string) (map[string]any, error) {
	if data == "" {
		return nil, ErrNoHangar
	}
	var hangar map[string]any
	if err := json.Unmarshal([]byte(data), &hangar); err != nil || hangar == nil {
		return nil, fmt.Errorf("the stored hangar could not be read: %v", err)
	}
	return hangar, nil
}

// saveHangar writes an edited hangar, and moves sp into the warchest, inside a transaction that holds the run's lock.
func (s *Store) saveHangar(ctx context.Context, tx *sql.Tx, playthroughID, campaign string, hangar map[string]any, sp int) error {
	b, err := json.Marshal(hangar)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, s.q(`UPDATE campaign_runs SET hangar = ?, warchest = warchest + ? WHERE playthrough_id = ? AND campaign = ?`), string(b), sp, playthroughID, campaign)
	return err
}

// The one-time import of the hangars MekBay used to hold (see internal/site/hangarimport.go). Before hangars lived here, a run's hangar was a force in MekBay
// (campaign_runs.hangar_instance) and each checkpoint held a frozen copy of it there (campaign_checkpoints.hangar_copy). Those columns stay for the import only.

// HangarImport is a hangar still in MekBay: a run's (Seq 0) or a checkpoint's.
type HangarImport struct {
	PlaythroughID, Campaign string
	Seq                     int    // the checkpoint's sequence number; 0 for the run's own hangar
	Instance                string // the force in MekBay
}

// RunHangarImports lists the runs whose hangar is still only in MekBay: none stored, and a MekBay force to read it from.
func (s *Store) RunHangarImports(ctx context.Context) ([]HangarImport, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT playthrough_id, campaign, hangar_instance FROM campaign_runs WHERE hangar = '' AND hangar_instance <> '' ORDER BY playthrough_id, campaign`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HangarImport
	for rows.Next() {
		var h HangarImport
		if err := rows.Scan(&h.PlaythroughID, &h.Campaign, &h.Instance); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// CheckpointHangarImports lists the checkpoints whose hangar is still only in MekBay: a frozen copy there, and none in the snapshot.
func (s *Store) CheckpointHangarImports(ctx context.Context) ([]HangarImport, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT playthrough_id, campaign, seq, hangar_copy, snapshot FROM campaign_checkpoints WHERE hangar_copy <> '' ORDER BY playthrough_id, campaign, seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HangarImport
	for rows.Next() {
		var h HangarImport
		var snap string
		if err := rows.Scan(&h.PlaythroughID, &h.Campaign, &h.Seq, &h.Instance, &snap); err != nil {
			return nil, err
		}
		if snapshotHangar(snap) == "" {
			out = append(out, h)
		}
	}
	return out, rows.Err()
}

// snapshotHangar is the hangar a snapshot's run row holds ("" if it has none: a snapshot from before hangars were stored here).
func snapshotHangar(raw string) string {
	snap, err := decodeSnapshot(raw)
	if err != nil || len(snap["campaign_runs"]) == 0 {
		return ""
	}
	h, _ := snap["campaign_runs"][0]["hangar"].(string)
	return h
}

// SetCheckpointHangar puts a hangar into a checkpoint's snapshot, as if it had been taken with it (sql.ErrNoRows if there is no such checkpoint).
func (s *Store) SetCheckpointHangar(ctx context.Context, playthroughID, campaign string, seq int, data []byte) error {
	if !json.Valid(data) {
		return errors.New("the hangar is not valid JSON")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	if err := tx.QueryRowContext(ctx, s.q(`SELECT snapshot FROM campaign_checkpoints WHERE playthrough_id = ? AND campaign = ? AND seq = ?`), playthroughID, campaign, seq).Scan(&raw); err != nil {
		return err
	}
	// Decoded with its numbers as they are (json.Number), so they are written back unchanged.
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var snap map[string][]map[string]any
	if err := dec.Decode(&snap); err != nil {
		return fmt.Errorf("the checkpoint could not be read: %w", err)
	}
	if len(snap["campaign_runs"]) != 1 {
		return errors.New("the checkpoint has no campaign row")
	}
	snap["campaign_runs"][0]["hangar"] = string(data)
	b, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, s.q(`UPDATE campaign_checkpoints SET snapshot = ? WHERE playthrough_id = ? AND campaign = ? AND seq = ?`), string(b), playthroughID, campaign, seq); err != nil {
		return err
	}
	return tx.Commit()
}
