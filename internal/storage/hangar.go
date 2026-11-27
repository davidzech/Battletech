package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
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

// BattleInProgressError is a hangar edit refused because a battle is in progress in the run (see BattleGuard): Scenario is its mission's ID.
type BattleInProgressError struct{ Scenario string }

func (e BattleInProgressError) Error() string { return "a battle is in progress (" + e.Scenario + ")" }

// BattleGuard says which battles a hangar edit may be made during. A battle is in progress while its operation is recorded (mekbay_operations) for a mission
// not completed, or while a start holds its claim (ClaimBattle).
type BattleGuard struct {
	AllowClaim string                     // the scenario whose start claim the edit belongs to (Start battle's own reconfiguration); "" allows none
	Counts     func(scenario string) bool // which scenarios' battles count (the site leaves out a scenario its campaign no longer has); nil counts every one
}

// EditHangar is EditHangarGuarded refusing the edit during any battle.
func (s *Store) EditHangar(ctx context.Context, playthroughID, campaign string, edit HangarEdit) (warchest int, err error) {
	return s.EditHangarGuarded(ctx, playthroughID, campaign, BattleGuard{}, edit)
}

// EditHangarGuarded changes a campaign run's hangar and its warchest together, in one transaction, and returns the warchest it left. The transaction takes the
// run's row lock first (lockRun), so edits made at once, on one server or several sharing the database, take turns and each starts from the hangar the last one
// saved. With the lock held it checks that no battle the guard doesn't allow is in progress (BattleInProgressError): a request checks the battle lock before it
// gets here, and a battle started since must not be built from a hangar changed under it. A charge the warchest can't cover changes nothing and returns
// ErrInsufficientFunds with the warchest as it is; an error from edit changes nothing and is returned as it is. sql.ErrNoRows if the campaign isn't started,
// ErrNoHangar if it has no hangar.
func (s *Store) EditHangarGuarded(ctx context.Context, playthroughID, campaign string, guard BattleGuard, edit HangarEdit) (warchest int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := s.lockRun(ctx, tx, playthroughID, campaign); err != nil {
		return 0, err
	}
	if err := s.checkBattleGuard(ctx, tx, playthroughID, campaign, guard); err != nil {
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

// checkBattleGuard answers BattleInProgressError when the run has a battle in progress that the guard doesn't allow, read inside the transaction that holds the
// run's lock: ClaimBattle updates the run's row, so a claim made after the lock waits for this edit, and a start records its operation while its claim holds.
func (s *Store) checkBattleGuard(ctx context.Context, tx *sql.Tx, playthroughID, campaign string, guard BattleGuard) error {
	counts := func(id string) bool { return guard.Counts == nil || guard.Counts(id) }
	rows, err := tx.QueryContext(ctx, s.q(`SELECT o.scenario_id FROM mekbay_operations o
		WHERE o.playthrough_id = ? AND o.campaign = ? AND NOT EXISTS (SELECT 1 FROM scenario_completions c
			WHERE c.playthrough_id = o.playthrough_id AND c.campaign = o.campaign AND c.scenario_id = o.scenario_id)
		ORDER BY o.scenario_id`), playthroughID, campaign)
	if err != nil {
		return err
	}
	var battles []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		battles = append(battles, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range battles {
		if counts(id) {
			return BattleInProgressError{id}
		}
	}
	var at int64
	var claimed string
	if err := tx.QueryRowContext(ctx, s.q(`SELECT battle_claim, battle_claim_scenario FROM campaign_runs WHERE playthrough_id = ? AND campaign = ?`),
		playthroughID, campaign).Scan(&at, &claimed); err != nil {
		return err
	}
	if at >= time.Now().Add(-BattleClaimTTL).Unix() && claimed != guard.AllowClaim && counts(claimed) {
		return BattleInProgressError{claimed}
	}
	return nil
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
// (campaign_runs.hangar_instance) and each checkpoint held a frozen copy of it there (campaign_checkpoints.hangar_copy). Those columns are for the import only:
// once a hangar is imported, or skipped as one that can't be, its column is cleared in the same write, so it is never listed or read from MekBay again. A
// follow-up drops the columns together with the import.

// HangarImport is a hangar still in MekBay: a run's (Seq 0) or a checkpoint's.
type HangarImport struct {
	PlaythroughID, Campaign string
	Seq                     int    // the checkpoint's sequence number; 0 for the run's own hangar
	Instance                string // the force in MekBay
	// SourceURL is the run's hangar link when the site kept its own copy of the player's force (an empty one: a run from before that, whose hangar is the
	// player's own force, crews and all). Only for a run's import.
	SourceURL string
}

// ErrBadSnapshot is a checkpoint whose snapshot can't take a hangar: it can't be decoded, or has no campaign row. Importing again can't fix it.
var ErrBadSnapshot = errors.New("the checkpoint's snapshot can't be read")

// snapshotWithHangar matches (LIKE) a checkpoint snapshot whose run row holds a hangar: snapshots are JSON with sorted keys, the hangar a JSON object kept as a
// string, so its first characters are always `"hangar":"{`. (A snapshot from before hangars were stored here has "hangar":"" or no hangar at all.)
const snapshotWithHangar = `%"hangar":"{%`

// ForgetImportedHangarSources clears the MekBay forces of the runs and checkpoints whose hangar is already stored (imported by an earlier version of the import,
// which left them set), so they are not listed again.
func (s *Store) ForgetImportedHangarSources(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE campaign_runs SET hangar_instance = '' WHERE hangar <> '' AND hangar_instance <> ''`); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE campaign_checkpoints SET hangar_copy = '' WHERE hangar_copy <> '' AND snapshot LIKE ?`), snapshotWithHangar)
	return err
}

// RunHangarImports lists the runs whose hangar is still only in MekBay: none stored, and a MekBay force to read it from.
func (s *Store) RunHangarImports(ctx context.Context) ([]HangarImport, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT playthrough_id, campaign, hangar_instance, source_url FROM campaign_runs WHERE hangar = '' AND hangar_instance <> '' ORDER BY playthrough_id, campaign`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HangarImport
	for rows.Next() {
		var h HangarImport
		if err := rows.Scan(&h.PlaythroughID, &h.Campaign, &h.Instance, &h.SourceURL); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// CheckpointHangarImports lists the checkpoints whose hangar is still only in MekBay: a frozen copy there, and none in the snapshot.
func (s *Store) CheckpointHangarImports(ctx context.Context) ([]HangarImport, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT playthrough_id, campaign, seq, hangar_copy FROM campaign_checkpoints WHERE hangar_copy <> '' AND snapshot NOT LIKE ?
		ORDER BY playthrough_id, campaign, seq`), snapshotWithHangar)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HangarImport
	for rows.Next() {
		var h HangarImport
		if err := rows.Scan(&h.PlaythroughID, &h.Campaign, &h.Seq, &h.Instance); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ImportHangar stores a run's hangar read from MekBay and forgets its MekBay force, in one write, but only into a run that has no hangar: stored is false when it
// has one (another server imported it first), and nothing is changed then.
func (s *Store) ImportHangar(ctx context.Context, playthroughID, campaign string, data []byte) (stored bool, err error) {
	if !json.Valid(data) {
		return false, errors.New("the hangar is not valid JSON")
	}
	res, err := s.db.ExecContext(ctx, s.q(`UPDATE campaign_runs SET hangar = ?, hangar_instance = '' WHERE playthrough_id = ? AND campaign = ? AND hangar = ''`),
		string(data), playthroughID, campaign)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// SkipHangarImport forgets the MekBay force of a run whose hangar can't be imported (MekBay no longer has it, or its data can't be read), so it is not tried
// again. The run is left without a hangar.
func (s *Store) SkipHangarImport(ctx context.Context, playthroughID, campaign string) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE campaign_runs SET hangar_instance = '' WHERE playthrough_id = ? AND campaign = ?`), playthroughID, campaign)
	return err
}

// ImportCheckpointHangar puts a hangar read from MekBay into a checkpoint's snapshot, as if it had been taken with it, and forgets the checkpoint's frozen copy,
// in one write, but only into a snapshot without a hangar: stored is false when it has one (or another server changed it meanwhile). sql.ErrNoRows if there
// is no such checkpoint, ErrBadSnapshot if its snapshot can't take a hangar.
func (s *Store) ImportCheckpointHangar(ctx context.Context, playthroughID, campaign string, seq int, data []byte) (stored bool, err error) {
	if !json.Valid(data) {
		return false, errors.New("the hangar is not valid JSON")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var raw string
	if err := tx.QueryRowContext(ctx, s.q(`SELECT snapshot FROM campaign_checkpoints WHERE playthrough_id = ? AND campaign = ? AND seq = ?`), playthroughID, campaign, seq).Scan(&raw); err != nil {
		return false, err
	}
	// Decoded with its numbers as they are (json.Number), so they are written back unchanged.
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var snap map[string][]map[string]any
	if err := dec.Decode(&snap); err != nil {
		return false, fmt.Errorf("%w: %v", ErrBadSnapshot, err)
	}
	if len(snap["campaign_runs"]) != 1 {
		return false, fmt.Errorf("%w: it has no campaign row", ErrBadSnapshot)
	}
	if h, _ := snap["campaign_runs"][0]["hangar"].(string); h != "" {
		_, err := tx.ExecContext(ctx, s.q(`UPDATE campaign_checkpoints SET hangar_copy = '' WHERE playthrough_id = ? AND campaign = ? AND seq = ?`), playthroughID, campaign, seq)
		if err != nil {
			return false, err
		}
		return false, tx.Commit()
	}
	snap["campaign_runs"][0]["hangar"] = string(data)
	b, err := json.Marshal(snap)
	if err != nil {
		return false, err
	}
	// Only over the snapshot just read: a checkpoint another server filled (or rolled back past) since is left as it is.
	res, err := tx.ExecContext(ctx, s.q(`UPDATE campaign_checkpoints SET snapshot = ?, hangar_copy = '' WHERE playthrough_id = ? AND campaign = ? AND seq = ? AND snapshot = ?`),
		string(b), playthroughID, campaign, seq, raw)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, tx.Commit()
}

// SkipCheckpointHangarImport forgets the frozen copy of a checkpoint whose hangar can't be imported, so it is not tried again. Rolling back to the checkpoint is
// refused then (RestoreCheckpoint).
func (s *Store) SkipCheckpointHangarImport(ctx context.Context, playthroughID, campaign string, seq int) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE campaign_checkpoints SET hangar_copy = '' WHERE playthrough_id = ? AND campaign = ? AND seq = ?`), playthroughID, campaign, seq)
	return err
}
