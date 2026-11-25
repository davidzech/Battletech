package site

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/storage"
)

// manage serves /<campaign>/manage/, the page for managing a started campaign's force: hangar link, named pilots and
// deleting the campaign. A campaign that isn't started (or a visitor whose account isn't named) is sent back to the campaign page,
// where it is started.
func (s *Server) manage(w http.ResponseWriter, r *http.Request) {
	c, ok := s.lib.Campaign(r.PathValue("campaign"))
	if !ok {
		s.notFound(w, r)
		return
	}
	back := "/" + c.Slug + "/"
	seed := currentSeed(r)
	if seed == "" {
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	run, _, err := s.store.Run(r.Context(), playthroughID(seed), c.Slug)
	if err != nil {
		http.Error(w, "could not load campaign", http.StatusInternalServerError)
		return
	}
	if !run.Started() {
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	// The scenario page's link here carries ?back=<scenario>, so Back returns to that scenario. Only a scenario of this campaign is honoured: the value is looked up,
	// never echoed into the page as a URL.
	p := Page{
		Title:    "Manage force | " + c.Label,
		Crumbs:   []Crumb{{Label: c.Label, URL: back}, {Label: "Manage", URL: back + "manage/"}},
		Campaign: c,
	}
	if sc, ok := c.Scenario(r.URL.Query().Get("back")); ok {
		p.BackURL = back + sc.Slug + "/"
		p.BackLabel = "Back to " + c.MissionName(*sc)
	}
	p.OnManagePage = true
	s.render(w, r, http.StatusOK, "manage", p)
}

// forceJSON describes the units in a started campaign's hangar for the campaign overview, read anonymously from MekBay (see
// forceData), and keeps the copy for the page's next visit (see hangarcache.go).
func (s *Server) forceJSON(w http.ResponseWriter, r *http.Request) {
	seed := currentSeed(r)
	if seed == "" {
		http.Error(w, "Choose a seed first.", http.StatusBadRequest)
		return
	}
	c, ok := s.lib.Campaign(r.FormValue("campaign"))
	if !ok {
		http.Error(w, "unknown campaign", http.StatusNotFound)
		return
	}
	run, started, err := s.store.Run(r.Context(), playthroughID(seed), c.Slug)
	if err != nil {
		http.Error(w, "could not load campaign", http.StatusInternalServerError)
		return
	}
	if !started {
		http.Error(w, "Start this campaign first.", http.StatusBadRequest)
		return
	}
	h, err := s.mekbay.ReadHangar(r.Context(), run.HangarInstance)
	if err != nil {
		log.Printf("read hangar for the overview: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.cacheHangar(r.Context(), playthroughID(seed), c.Slug, run.HangarInstance, h)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(forceData(h, c))
}

// setPlayers is POST /api/campaign/players: change how many players a started campaign is played with (the Manage page's Overview). The count is only the
// Deploy wizard's default, which can still be changed for each deploy, but it is held to the same rule as at the start: within the campaign's range.
func (s *Server) setPlayers(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	n, err := parseStartingPlayers(r.PostFormValue("players"), c)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.store.SetRunPlayers(r.Context(), pt.ID, c.Slug, n); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "Start this campaign first.", http.StatusBadRequest)
		return
	} else if err != nil {
		http.Error(w, "could not save the player count", http.StatusInternalServerError)
		return
	}
	s.logHistory(r, pt.ID, c.Slug, storage.HistoryCampaign, 0, fmt.Sprintf("The default number of players was set to %d.", n))
	w.WriteHeader(http.StatusNoContent)
}

// setForceName is POST /api/campaign/force-name: rename a started campaign's force (the Manage page's Overview).
func (s *Server) setForceName(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	name, err := content.ParseForceName(r.PostFormValue("name"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.store.SetForceName(r.Context(), pt.ID, c.Slug, name); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "Start this campaign first.", http.StatusBadRequest)
		return
	} else if err != nil {
		http.Error(w, "could not save the force name", http.StatusInternalServerError)
		return
	}
	s.logHistory(r, pt.ID, c.Slug, storage.HistoryCampaign, 0, "The force was renamed "+name+".")
	w.WriteHeader(http.StatusNoContent)
}
