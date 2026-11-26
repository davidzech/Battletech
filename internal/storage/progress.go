package storage

import (
	"context"
)

// Completions returns the slugs of the scenarios a playthrough has completed in a campaign.
func (s *Store) Completions(ctx context.Context, playthroughID, campaign string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT scenario_id FROM scenario_completions WHERE playthrough_id = ? AND campaign = ?`), playthroughID, campaign)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// SetScenarioCompleted marks a scenario completed or not, and reports whether that changed anything (false when it already was as asked, so a repeated request
// is not logged or checkpointed again). Completing also forgets the scenario's battle, in the same transaction: a completed mission has no battle in progress.
func (s *Store) SetScenarioCompleted(ctx context.Context, playthroughID, campaign, scenario string, done bool) (changed bool, err error) {
	if !done {
		res, err := s.db.ExecContext(ctx, s.q(`DELETE FROM scenario_completions WHERE playthrough_id = ? AND campaign = ? AND scenario_id = ?`), playthroughID, campaign, scenario)
		if err != nil {
			return false, err
		}
		n, err := res.RowsAffected()
		return n > 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, s.q(`INSERT INTO scenario_completions (playthrough_id, campaign, scenario_id) VALUES (?, ?, ?)
		ON CONFLICT (playthrough_id, campaign, scenario_id) DO NOTHING`), playthroughID, campaign, scenario)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM mekbay_operations WHERE playthrough_id = ? AND campaign = ? AND scenario_id = ?`), playthroughID, campaign, scenario); err != nil {
		return false, err
	}
	return n > 0, tx.Commit()
}
