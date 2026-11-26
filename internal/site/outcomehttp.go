package site

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/storage"
)

// The outcome engine's endpoints: previewing and committing a mission's outcome (see outcomes.go).

// outcomeRequest checks a scenario-outcome request and evaluates it: a same-origin request from a started campaign, for an available scenario with an outcome engine
// that is not completed yet. It returns what the engine makes of the reported inputs.
func (s *Server) outcomeRequest(w http.ResponseWriter, r *http.Request) (pt storage.Playthrough, c *content.Campaign, sc *content.Scenario, res content.OutcomeResult, ok bool) {
	if pt, c, _, ok = s.openRunRequest(w, r); !ok {
		return
	}
	ok = false
	sc, found := c.Scenario(r.PostFormValue("scenario"))
	if !found {
		http.Error(w, "unknown scenario", http.StatusNotFound)
		return
	}
	if !s.battleLock(w, r, pt, c, sc.ID) { // a battle in progress locks every other mission
		return
	}
	if !sc.HasOutcome() {
		http.Error(w, "This mission has no outcome to report.", http.StatusBadRequest)
		return
	}
	completed, err := s.store.Completions(r.Context(), pt.ID, c.Slug)
	if err != nil {
		http.Error(w, "could not load the campaign log", http.StatusInternalServerError)
		return
	}
	if completed[sc.Slug] {
		http.Error(w, "This mission is already completed.", http.StatusConflict)
		return
	}
	if !c.Available(completed)[sc.Slug] {
		http.Error(w, "That scenario isn't available: it doesn't follow your last completed mission.", http.StatusConflict)
		return
	}
	var in content.OutcomeInputs
	if err := json.Unmarshal([]byte(r.PostFormValue("inputs")), &in); err != nil {
		http.Error(w, "The outcome form could not be read.", http.StatusBadRequest)
		return
	}
	pilots, err := s.store.Pilots(r.Context(), pt.ID, c.Slug)
	if err != nil {
		http.Error(w, "could not load your pilots", http.StatusInternalServerError)
		return
	}
	names, ranks := map[string]string{}, map[string]string{}
	for _, p := range pilots {
		if !p.Fallen {
			names[p.ID], ranks[p.ID] = p.Name, p.Rank
		}
	}
	res, err = sc.EvaluateOutcome(in, names)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	res.ResolvePromotions(ranks, c.Ranks) // "at least": a pilot already at or above a promotion keeps their rank
	return pt, c, sc, res, true
}

// previewOutcome is POST /api/campaign/scenario-outcome: what the reported inputs would do, without changing anything.
func (s *Server) previewOutcome(w http.ResponseWriter, r *http.Request) {
	_, c, _, res, ok := s.outcomeRequest(w, r)
	if !ok {
		return
	}
	res.KeywordNames = c.KeywordNames(res.Keywords)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(res)
}

// commitOutcome completes a scenario that has an outcome engine: the same request as the preview, applied.
func (s *Server) commitOutcome(w http.ResponseWriter, r *http.Request) {
	pt, c, sc, res, ok := s.outcomeRequest(w, r)
	if !ok {
		return
	}
	if res.MVPRefused != "" {
		http.Error(w, res.MVPRefused+" leaves the campaign after this mission, so they cannot be the MVP.", http.StatusBadRequest)
		return
	}
	if res.MVPMissing() {
		http.Error(w, "Choose the mission's MVP: every mission has one when a named pilot took part and stays in the campaign.", http.StatusBadRequest)
		return
	}
	if !s.writeBackBattle(w, r, pt, c, sc) {
		return
	}
	// Two stages for the hangar: its frozen copy is made now, and the checkpoint that holds it is recorded once the completion is saved.
	copyID, cerr := "", error(nil)
	if run, ok, err := s.store.Run(r.Context(), pt.ID, c.Slug); err != nil {
		http.Error(w, "could not load campaign", http.StatusInternalServerError)
		return
	} else if ok {
		copyID, cerr = s.checkpointCopy(r.Context(), currentSeed(r), run)
	}
	if cerr != nil {
		http.Error(w, "Could not save a checkpoint of your hangar in MekBay: "+cerr.Error(), http.StatusBadGateway)
		return
	}
	label := "Completed " + c.MissionName(*sc)
	switch err := s.store.CompleteWithOutcome(r.Context(), pt.ID, c.Slug, sc.Slug, res); {
	case errors.Is(err, storage.ErrAlreadyCompleted):
		http.Error(w, "This mission is already completed.", http.StatusConflict)
	case errors.Is(err, storage.ErrOutcomePilot):
		http.Error(w, "One of the pilots is no longer a living named pilot. Reload and report again.", http.StatusConflict)
	case errors.Is(err, storage.ErrOutcomeFunds):
		http.Error(w, "That outcome would take the warchest below zero.", http.StatusConflict)
	case err != nil:
		http.Error(w, "could not save the outcome", http.StatusInternalServerError)
	default:
		s.logHistory(r, pt.ID, c.Slug, storage.HistoryMission, res.Warchest, outcomeHistory(c, sc, res)) // before the checkpoint, so rolling back to it keeps the line
		s.recordCheckpoint(r.Context(), currentSeed(r), pt.ID, c.Slug, label, copyID)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if copyID != "" {
		s.deleteHangars(r.Context(), currentSeed(r), []string{copyID}) // the completion was not saved: the staged copy is not needed
	}
}

// writeBackBattle, for a mission fought with the player's own force (not a provided one) that has a battle in progress, writes the battle's units back into the
// hangar before the mission is completed (see commitbattle.go), so a failure leaves the mission open to try again (writing the same state twice changes nothing). It
// answers the request itself and returns false when it fails.
func (s *Server) writeBackBattle(w http.ResponseWriter, r *http.Request, pt storage.Playthrough, c *content.Campaign, sc *content.Scenario) bool {
	if sc.Provides() {
		return true
	}
	op, had, err := s.store.MekBayOperation(r.Context(), pt.ID, c.Slug, sc.ID)
	if err != nil {
		http.Error(w, "could not load the battle", http.StatusInternalServerError)
		return false
	}
	run, started, err := s.store.Run(r.Context(), pt.ID, c.Slug)
	if err != nil {
		http.Error(w, "could not load campaign", http.StatusInternalServerError)
		return false
	}
	if !had || !started || !run.Owned() {
		return true // no battle to write back, or a hangar the site doesn't own
	}
	rep, err := s.mekbay.CommitBattle(r.Context(), mekbay.CommitBattleRequest{OwnerUID: mekbay.AccountUID(currentSeed(r), mekbay.SlotHangar), HangarInstance: run.HangarInstance, OperationID: op.OperationID, Skip: skipAllies(op.AlliesInstance)})
	if err != nil {
		http.Error(w, "Could not write the battle's damage back into your hangar: "+err.Error(), http.StatusBadGateway)
		return false
	}
	if len(rep.Skipped) > 0 {
		log.Printf("battle write-back: %d units written, no match in the hangar for %v", rep.Matched, rep.Skipped)
	}
	return true
}
