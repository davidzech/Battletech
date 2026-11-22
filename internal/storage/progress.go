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

// SetScenarioCompleted marks a scenario completed or not.
func (s *Store) SetScenarioCompleted(ctx context.Context, playthroughID, campaign, scenario string, done bool) error {
	if !done {
		_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM scenario_completions WHERE playthrough_id = ? AND campaign = ? AND scenario_id = ?`), playthroughID, campaign, scenario)
		return err
	}
	if _, err := s.db.ExecContext(ctx, s.q(`INSERT INTO scenario_completions (playthrough_id, campaign, scenario_id) VALUES (?, ?, ?)
		ON CONFLICT (playthrough_id, campaign, scenario_id) DO NOTHING`), playthroughID, campaign, scenario); err != nil {
		return err
	}
	_, err := s.DeleteMekBayOperation(ctx, playthroughID, campaign, scenario) // a completed mission has no battle in progress
	return err
}
