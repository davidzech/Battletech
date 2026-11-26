package site

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/storage"
)

// The campaign log: which of a campaign's scenarios the run has completed. The first scenario (000) is available from the start, and completing a scenario
// makes the ones it branches to available (Scenario.Branches). Locked scenarios are left out of the lists and menus; a "Show all scenarios" box on the campaign page
// reveals them, and the scenario page's own link still opens any of them.

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
func (s *Server) progress(r *http.Request, c *content.Campaign) (completed, available map[string]bool) {
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
	pt, c, _, ok := s.openRunRequest(w, r)
	if !ok {
		return
	}
	sc, found := c.Scenario(r.PostFormValue("scenario"))
	if !found {
		http.Error(w, "unknown scenario", http.StatusNotFound)
		return
	}
	done := r.PostFormValue("done") == "1"
	// A battle in progress locks the campaign log, except to complete that battle's own mission, which ends it.
	allowed := ""
	if done {
		allowed = sc.ID
	}
	if !s.battleLock(w, r, pt, c, allowed) {
		return
	}
	if sc.HasOutcome() {
		// A mission with an outcome is completed by reporting it (POST /api/campaign/scenario-outcome/commit), which gives the rewards; marking it by hand would skip
		// them, and un-completing it would let them be given twice.
		if done {
			http.Error(w, "Report this mission's outcome to complete it.", http.StatusBadRequest)
			return
		}
		if rec, was, err := s.store.CompletionOutcome(r.Context(), pt.ID, c.Slug, sc.Slug); err != nil {
			http.Error(w, "could not load the campaign log", http.StatusInternalServerError)
			return
		} else if was && rec != nil {
			http.Error(w, "This mission's outcome has been committed. To correct it, adjust the pilots and funds under Manage force.", http.StatusConflict)
			return
		}
	}
	if done {
		completed, err := s.store.Completions(r.Context(), pt.ID, c.Slug)
		if err != nil {
			http.Error(w, "could not load the campaign log", http.StatusInternalServerError)
			return
		}
		if !c.Available(completed)[sc.Slug] {
			http.Error(w, "That scenario isn't available: it doesn't follow your last completed mission.", http.StatusConflict)
			return
		}
	}
	if err := s.store.SetScenarioCompleted(r.Context(), pt.ID, c.Slug, sc.Slug, done); err != nil {
		http.Error(w, "could not save the campaign log", http.StatusInternalServerError)
		return
	}
	if done {
		s.logHistory(r, pt.ID, c.Slug, storage.HistoryMission, 0, "Completed "+c.MissionName(*sc)+".")
	} else {
		s.logHistory(r, pt.ID, c.Slug, storage.HistoryMission, 0, "Marked "+c.MissionName(*sc)+" not completed.")
	}
	if done { // a mission completed by hand is a rollback point too (recorded after the line above, so rolling back to it keeps the line)
		if run, ok, err := s.store.Run(r.Context(), pt.ID, c.Slug); err == nil && ok {
			if copyID, err := s.checkpointCopy(r.Context(), currentSeed(r), run); err != nil {
				log.Printf("checkpoint after %s: %v", sc.Slug, err)
			} else {
				s.recordCheckpoint(r.Context(), currentSeed(r), pt.ID, c.Slug, "Completed "+c.MissionName(*sc), copyID)
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// recordedOutcome is the stored report of a completed scenario's outcome, for the scenario page (nil if there is none).
func (s *Server) recordedOutcome(r *http.Request, c *content.Campaign, sc *content.Scenario, completed map[string]bool) *content.OutcomeResult {
	seed := currentSeed(r)
	if seed == "" || !sc.HasOutcome() || !completed[sc.Slug] {
		return nil
	}
	res, _, err := s.store.CompletionOutcome(r.Context(), playthroughID(seed), c.Slug, sc.Slug)
	if err != nil {
		return nil
	}
	return res
}

// OutcomeInputsJSON is the scenario's outcome inputs for the completion dialog.
func (p Page) OutcomeInputsJSON() string {
	if p.Scenario == nil {
		return "[]"
	}
	b, err := json.Marshal(p.Scenario.OutcomeInputs)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// OutcomePilotsJSON is the living named pilots, for the completion dialog's list of who took part.
func (p Page) OutcomePilotsJSON() string {
	type pilot struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Portrait string `json:"portrait"`
		Rank     string `json:"rank"`
	}
	out := []pilot{}
	for _, v := range p.Pilots {
		if !v.Fallen {
			out = append(out, pilot{ID: v.ID, Name: v.Name, Portrait: v.PortraitURL, Rank: v.Rank})
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "[]"
	}
	return string(b)
}
