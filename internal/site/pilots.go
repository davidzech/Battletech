package site

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// pilotRequest is what every named-pilot endpoint needs first: a same-origin request from a seed whose company is
// named and which has started the campaign the request names. It writes the error response itself and reports
// ok=false.
func (s *Server) pilotRequest(w http.ResponseWriter, r *http.Request) (pt Playthrough, c *Campaign, ok bool) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)
		return
	}
	seed := currentSeed(r)
	if seed == "" {
		http.Error(w, "Choose a seed first.", http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	c, found := s.lib.Campaign(r.PostFormValue("campaign"))
	if !found {
		http.Error(w, "unknown campaign", http.StatusNotFound)
		return
	}
	pt, found, err := s.store.Playthrough(r.Context(), playthroughID(seed))
	if err != nil {
		http.Error(w, "could not load playthrough", http.StatusInternalServerError)
		return
	}
	if !found || !pt.Configured() {
		http.Error(w, "Name your company first.", http.StatusBadRequest)
		return
	}
	if _, started, err := s.store.Run(r.Context(), pt.ID, c.Slug); err != nil {
		http.Error(w, "could not load campaign", http.StatusInternalServerError)
		return
	} else if !started {
		http.Error(w, "Start this campaign first.", http.StatusBadRequest)
		return
	}
	return pt, c, true
}

// addPilot names a new named pilot for the campaign. They start with no SP: SP is earned, then allocated.
func (s *Server) addPilot(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	name, err := parsePilotName(r.PostFormValue("name"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := s.store.AddPilot(r.Context(), pt.ID, c.Slug, name, SPAllocation{}, maxNamedPilots); errors.Is(err, ErrPilotLimit) {
		http.Error(w, fmt.Sprintf("You can have at most %d named pilots at a time.", maxNamedPilots), http.StatusBadRequest)
		return
	} else if err != nil {
		http.Error(w, "could not save the pilot", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// maxSPAward keeps a typo from handing a pilot a absurd amount of SP in one go.
const maxSPAward = 10000

// parseSP reads one SP amount from a form: blank means none.
func parseSP(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 || n > maxSPAward {
		return 0, fmt.Errorf("SP must be a whole number from 0 to %d.", maxSPAward)
	}
	return n, nil
}

// awardSP gives a pilot SP, allocated across their columns as the player chooses. (The book caps a pilot at 100 SP
// per track, but campaigns set their own rewards, e.g. Refusal War's trial pays up to 900, so no cap is enforced.)
func (s *Server) awardSP(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	var a SPAllocation
	for field, dst := range map[string]*int{
		"gunnery": &a.Gunnery, "piloting": &a.Piloting, "edge_tokens": &a.EdgeTokens, "edge_abilities": &a.EdgeAbilities,
	} {
		n, err := parseSP(r.PostFormValue(field))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		*dst = n
	}
	if a.Total() == 0 {
		http.Error(w, "Give the pilot some SP to allocate.", http.StatusBadRequest)
		return
	}
	if err := s.store.AwardSP(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot"), a); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "No such living pilot.", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "could not save the SP", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pilotFallen records that a named pilot died. They are permanently removed from the campaign.
func (s *Server) pilotFallen(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	if err := s.store.MarkFallen(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot")); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "No such living pilot.", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "could not save", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
