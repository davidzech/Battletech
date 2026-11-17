package site

import (
	"context"
	"net/http"
)

// The campaign log: which of a campaign's scenarios the company has completed. The first scenario (000) is available from the start, and completing a scenario
// makes the ones it branches to available (Scenario.Branches). Locked scenarios are left out of the lists and menus; a "Show all scenarios" box on the campaign page
// reveals them, and the scenario page's own link still opens any of them.

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
	_, err := s.db.ExecContext(ctx, s.q(`INSERT INTO scenario_completions (playthrough_id, campaign, scenario_id) VALUES (?, ?, ?)
		ON CONFLICT (playthrough_id, campaign, scenario_id) DO NOTHING`), playthroughID, campaign, scenario)
	return err
}

// scenarioState is how a scenario shows in the lists: completed, available (gray) or locked (hidden until "Show all scenarios").
func scenarioState(slug string, completed, available map[string]bool) string {
	switch {
	case completed[slug]:
		return "completed"
	case available[slug]:
		return "available"
	}
	return "locked"
}

// progress loads the visitor's completed scenarios for a campaign (none without a seed or a started campaign) and the set that makes available.
func (s *Server) progress(r *http.Request, c *Campaign) (completed, available map[string]bool) {
	completed = map[string]bool{}
	if seed := currentSeed(r); seed != "" {
		id := playthroughID(seed)
		if run, ok, err := s.store.Run(r.Context(), id, c.Slug); err == nil && ok && run.Started() {
			if got, err := s.store.Completions(r.Context(), id, c.Slug); err == nil {
				completed = got
			}
		}
	}
	return completed, c.Available(completed)
}

// setScenarioCompleted is POST /api/campaign/scenario-complete: mark a scenario of a started campaign completed (done=1) or not (done=0). Only an available scenario
// can be completed; un-completing is always allowed. The log is just a log: un-completing a scenario leaves the ones after it alone.
func (s *Server) setScenarioCompleted(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	run, started, err := s.store.Run(r.Context(), pt.ID, c.Slug)
	if err != nil || !started || !run.Started() {
		http.Error(w, "Start this campaign first.", http.StatusBadRequest)
		return
	}
	sc, found := c.Scenario(r.PostFormValue("scenario"))
	if !found {
		http.Error(w, "unknown scenario", http.StatusNotFound)
		return
	}
	done := r.PostFormValue("done") == "1"
	if done {
		completed, err := s.store.Completions(r.Context(), pt.ID, c.Slug)
		if err != nil {
			http.Error(w, "could not load the campaign log", http.StatusInternalServerError)
			return
		}
		if !c.Available(completed)[sc.Slug] {
			http.Error(w, "That scenario isn't available yet: complete the one before it first.", http.StatusConflict)
			return
		}
	}
	if err := s.store.SetScenarioCompleted(r.Context(), pt.ID, c.Slug, sc.Slug, done); err != nil {
		http.Error(w, "could not save the campaign log", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
