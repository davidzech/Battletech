package site

import (
	"encoding/json"
	"log"
	"net/http"
)

// manage serves /<campaign>/manage/, the page for managing a started campaign's force: hangar link, named pilots and
// deleting the campaign. A campaign that isn't started (or a visitor with no company) is sent back to the campaign page,
// where it is started.
func (s *Server) manage(w http.ResponseWriter, r *http.Request) {
	c, ok := s.lib.Campaign(r.PathValue("campaign"))
	if !ok {
		s.notFound(w, r)
		return
	}
	back := "/" + c.Slug + "/"
	if seed := currentSeed(r); seed == "" {
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	} else if run, _, err := s.store.Run(r.Context(), playthroughID(seed), c.Slug); err != nil {
		http.Error(w, "could not load campaign", http.StatusInternalServerError)
		return
	} else if !run.Started() {
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	s.render(w, r, http.StatusOK, "manage", Page{
		Title:    "Manage force | " + c.Label,
		Crumbs:   []Crumb{{Label: c.Label, URL: back}, {Label: "Manage", URL: back + "manage/"}},
		Campaign: c,
	})
}

// forceJSON describes the units in a started campaign's hangar for the campaign overview, read anonymously from MekBay.
// bv is the sum of the base BV of every unit the catalog knows (an unknown unit adds nothing, a destroyed one still counts:
// it is still in the hangar).
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
	units := hangarViews(h)
	bv := 0
	for _, u := range units {
		if !u.Unknown {
			bv += u.BV
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"name": h.Name, "units": units, "bv": bv})
}
