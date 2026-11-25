package site

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/storage"
)

// The Deploy popup's endpoints: what every MekBay endpoint needs first, and the hangar as the popup lists it.

// mekbayRequest is what both MekBay endpoints need first: the seed's playthrough, the campaign and scenario named by
// the request, and the playthrough's run of that campaign (which holds its hangar). It writes the error response
// itself and reports ok=false.
func (s *Server) mekbayRequest(w http.ResponseWriter, r *http.Request) (pt storage.Playthrough, run storage.CampaignRun, c *content.Campaign, sc *content.Scenario, ok bool) {
	seed := currentSeed(r)
	if seed == "" {
		http.Error(w, "Choose a seed first.", http.StatusBadRequest)
		return
	}
	c, found := s.lib.Campaign(r.FormValue("campaign"))
	if !found {
		http.Error(w, "unknown campaign", http.StatusNotFound)
		return
	}
	sc, found = c.Scenario(r.FormValue("scenario"))
	if !found || len(sc.Forces) == 0 {
		http.Error(w, "unknown scenario, or it has no OpFor to deploy against", http.StatusNotFound)
		return
	}
	pt, run, ok = s.startedRun(w, r, seed, c)
	return pt, run, c, sc, ok
}

// hangarJSON lists the player's hangar for the deploy popup, with the scenario's BV limit.
func (s *Server) hangarJSON(w http.ResponseWriter, r *http.Request) {
	pt, run, c, sc, ok := s.mekbayRequest(w, r)
	if !ok {
		return
	}
	h, err := s.scenarioHangar(r.Context(), run, sc)
	if err != nil {
		log.Printf("read hangar for %s: %v", sc.ID, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	_, hasOp, err := s.store.MekBayOperation(r.Context(), pt.ID, c.Slug, sc.ID)
	if err != nil {
		http.Error(w, "could not load operation", http.StatusInternalServerError)
		return
	}
	if !sc.Provides() { // the scenario's own mechs are not the hangar, so they are never cached as it
		s.cacheHangar(r.Context(), pt.ID, c.Slug, run.HangarInstance, h)
	}
	out := deployData(h, c, sc, run.Owned(), hasOp)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}
