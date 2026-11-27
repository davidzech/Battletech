package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/davidzech/Battletech/internal/content"
)

// The outcome engine's storage: completing a mission with its outcome, the recorded report, free recruits and the keyword ledger (see internal/content/outcome.go).

// ErrAlreadyCompleted is returned when a mission that has already been completed is completed again.
var ErrAlreadyCompleted = errors.New("this mission is already completed")

// ErrOutcomePilot is returned when a pilot the outcome affects is no longer a living named pilot of the campaign.
var ErrOutcomePilot = errors.New("a pilot in the outcome is no longer a living named pilot")

// ErrOutcomeFunds is returned when an outcome would take the warchest below zero.
var ErrOutcomeFunds = errors.New("the outcome would take the warchest below zero")

// CompleteWithOutcome applies a mission's outcome and records the mission as completed, all in one transaction: SP to the pilots who earned it, pilots killed in
// action, promotions (a pilot's Rank, as ResolvePromotions settled it), the warchest and the free recruits, the keywords granted (into the campaign ledger), and the completion row holding the report. It fails with ErrAlreadyCompleted if the mission was already completed, so
// the rewards can never be given twice. writeBack, when not nil, changes the hangar in the same transaction (the battle's damage written back into it); the SP it
// returns go into the warchest with the outcome's.
func (s *Store) CompleteWithOutcome(ctx context.Context, playthroughID, campaign, scenario string, res content.OutcomeResult, writeBack HangarEdit) error {
	record, err := json.Marshal(res)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.lockRun(ctx, tx, playthroughID, campaign); err != nil { // before the pilots' rows, as everywhere
		return err
	}
	var n int
	if err := tx.QueryRowContext(ctx, s.q(`SELECT COUNT(*) FROM scenario_completions WHERE playthrough_id = ? AND campaign = ? AND scenario_id = ?`), playthroughID, campaign, scenario).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrAlreadyCompleted
	}
	for _, p := range res.Pilots {
		if p.SP > 0 {
			r, err := tx.ExecContext(ctx, s.q(`UPDATE named_pilots SET sp_total = sp_total + ? WHERE id = ? AND playthrough_id = ? AND campaign = ? AND fallen = 0`), p.SP, p.PilotID, playthroughID, campaign)
			if err != nil {
				return err
			}
			if c, _ := r.RowsAffected(); c == 0 {
				return ErrOutcomePilot
			}
		}
		if p.Rank != "" && !p.KIA {
			r, err := tx.ExecContext(ctx, s.q(`UPDATE named_pilots SET rank = ? WHERE id = ? AND playthrough_id = ? AND campaign = ? AND fallen = 0`), p.Rank, p.PilotID, playthroughID, campaign)
			if err != nil {
				return err
			}
			if c, _ := r.RowsAffected(); c == 0 {
				return ErrOutcomePilot
			}
		}
		if p.KIA {
			r, err := tx.ExecContext(ctx, s.q(`UPDATE named_pilots SET fallen = 1 WHERE id = ? AND playthrough_id = ? AND campaign = ? AND fallen = 0`), p.PilotID, playthroughID, campaign)
			if err != nil {
				return err
			}
			if c, _ := r.RowsAffected(); c == 0 {
				return ErrOutcomePilot
			}
		}
	}
	if writeBack != nil {
		var data string
		if err := tx.QueryRowContext(ctx, s.q(`SELECT hangar FROM campaign_runs WHERE playthrough_id = ? AND campaign = ?`), playthroughID, campaign).Scan(&data); err != nil {
			return err
		}
		hangar, err := decodeHangar(data)
		if err != nil {
			return err
		}
		sp, err := writeBack(hangar)
		if err != nil {
			return err
		}
		if err := s.saveHangar(ctx, tx, playthroughID, campaign, hangar, 0); err != nil {
			return err
		}
		res.Warchest += sp
	}
	if res.Warchest != 0 || res.FreeRecruits != 0 {
		r, err := tx.ExecContext(ctx, s.q(`UPDATE campaign_runs SET warchest = warchest + ?, free_recruits = free_recruits + ? WHERE playthrough_id = ? AND campaign = ? AND warchest + ? >= 0`), res.Warchest, res.FreeRecruits, playthroughID, campaign, res.Warchest)
		if err != nil {
			return err
		}
		if c, _ := r.RowsAffected(); c == 0 {
			return ErrOutcomeFunds
		}
	}
	for _, k := range res.Keywords {
		if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO campaign_keywords (playthrough_id, campaign, keyword, scenario_id) VALUES (?, ?, ?, ?) ON CONFLICT (playthrough_id, campaign, keyword) DO NOTHING`), playthroughID, campaign, k, scenario); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO scenario_completions (playthrough_id, campaign, scenario_id, outcome) VALUES (?, ?, ?, ?)`), playthroughID, campaign, scenario, string(record)); err != nil {
		return err
	}
	// The battle is over: the scenario has no operation in progress any more.
	if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM mekbay_operations WHERE playthrough_id = ? AND campaign = ? AND scenario_id = ?`), playthroughID, campaign, scenario); err != nil {
		return err
	}
	return tx.Commit()
}

// CompletionOutcome returns the recorded report of a completed mission (nil with ok when it was completed without an outcome); ok is false if it isn't completed.
func (s *Store) CompletionOutcome(ctx context.Context, playthroughID, campaign, scenario string) (res *content.OutcomeResult, ok bool, err error) {
	var raw string
	err = s.db.QueryRowContext(ctx, s.q(`SELECT outcome FROM scenario_completions WHERE playthrough_id = ? AND campaign = ? AND scenario_id = ?`), playthroughID, campaign, scenario).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if raw == "" {
		return nil, true, nil
	}
	var r content.OutcomeResult
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return nil, true, nil // a record this version can't read is still a completion
	}
	return &r, true, nil
}

// ConsumeFreeRecruit uses up one free recruit, if the run has one; it reports whether it did.
func (s *Store) ConsumeFreeRecruit(ctx context.Context, playthroughID, campaign string) (bool, error) {
	res, err := s.db.ExecContext(ctx, s.q(`UPDATE campaign_runs SET free_recruits = free_recruits - 1 WHERE playthrough_id = ? AND campaign = ? AND free_recruits > 0`), playthroughID, campaign)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// RestoreFreeRecruit gives a free recruit back, when the recruit it was used for failed.
func (s *Store) RestoreFreeRecruit(ctx context.Context, playthroughID, campaign string) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE campaign_runs SET free_recruits = free_recruits + 1 WHERE playthrough_id = ? AND campaign = ?`), playthroughID, campaign)
	return err
}

// LedgerEntry is a keyword in the campaign's ledger: which scenario granted it.
type LedgerEntry struct {
	Keyword  string
	Scenario string
}

// Ledger returns the keywords a playthrough's campaign has been granted, oldest first. Keywords belong to the campaign as a whole, not to any pilot.
func (s *Store) Ledger(ctx context.Context, playthroughID, campaign string) ([]LedgerEntry, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT keyword, scenario_id FROM campaign_keywords WHERE playthrough_id = ? AND campaign = ? ORDER BY granted_at, keyword`), playthroughID, campaign)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LedgerEntry
	for rows.Next() {
		var e LedgerEntry
		if err := rows.Scan(&e.Keyword, &e.Scenario); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetKeywords makes the campaign's ledger hold exactly these keywords, for correcting it by hand (the Manage page's
// Danger zone): keywords not listed are removed, and new ones are added with an empty scenario_id. Keywords
// already held keep the scenario that granted them. One transaction.
func (s *Store) SetKeywords(ctx context.Context, playthroughID, campaign string, keywords []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	want := map[string]bool{}
	for _, k := range keywords {
		want[k] = true
	}
	rows, err := tx.QueryContext(ctx, s.q(`SELECT keyword FROM campaign_keywords WHERE playthrough_id = ? AND campaign = ?`), playthroughID, campaign)
	if err != nil {
		return err
	}
	held := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return err
		}
		held[k] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for k := range held {
		if !want[k] {
			if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM campaign_keywords WHERE playthrough_id = ? AND campaign = ? AND keyword = ?`), playthroughID, campaign, k); err != nil {
				return err
			}
		}
	}
	for _, k := range keywords {
		if !held[k] {
			if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO campaign_keywords (playthrough_id, campaign, keyword, scenario_id) VALUES (?, ?, ?, '') ON CONFLICT (playthrough_id, campaign, keyword) DO NOTHING`), playthroughID, campaign, k); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
