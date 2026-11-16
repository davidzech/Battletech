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
	portrait := r.PostFormValue("portrait")
	if portrait != "" && !validPortrait(portrait) {
		http.Error(w, "Pick a portrait from the list.", http.StatusBadRequest)
		return
	}
	if portrait == "" {
		existing, err := s.store.Pilots(r.Context(), pt.ID, c.Slug)
		if err != nil {
			http.Error(w, "could not load named pilots", http.StatusInternalServerError)
			return
		}
		portrait = randomPortrait(livingPortraits(existing))
	}
	if _, err := s.store.AddPilot(r.Context(), pt.ID, c.Slug, name, portrait, SPAllocation{}, maxNamedPilots); errors.Is(err, ErrPilotLimit) {
		http.Error(w, fmt.Sprintf("You can have at most %d named pilots at a time.", maxNamedPilots), http.StatusBadRequest)
		return
	} else if errors.Is(err, ErrPortraitTaken) {
		http.Error(w, "Another pilot already has that portrait.", http.StatusBadRequest)
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

// parseStartingPilots reads the pilots a campaign's start wizard submits (fields pilot0_name, pilot0_gunnery, ...):
// exactly the campaign's number of starting pilots, each with a name of their own, and every one allocating all of
// the campaign's starting SP, as the book has a pilot allocate SP the moment they earn it.
func parseStartingPilots(r *http.Request, c *Campaign) ([]NewPilot, error) {
	var out []NewPilot
	seen := map[string]bool{}
	takenPortraits := map[string]bool{}
	for i := 0; i < c.StartingNamedPilots; i++ {
		prefix := "pilot" + strconv.Itoa(i) + "_"
		name, err := parsePilotName(r.PostFormValue(prefix + "name"))
		if err != nil {
			return nil, fmt.Errorf("Named pilot %d: %w", i+1, err)
		}
		if seen[strings.ToLower(name)] {
			return nil, errors.New("Two of your pilots can't share a name.")
		}
		seen[strings.ToLower(name)] = true
		var a SPAllocation
		for field, dst := range map[string]*int{
			"gunnery": &a.Gunnery, "piloting": &a.Piloting, "edge_tokens": &a.EdgeTokens, "edge_abilities": &a.EdgeAbilities,
		} {
			n, err := parseSP(r.PostFormValue(prefix + field))
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			*dst = n
		}
		if a.Total() != c.StartingPilotSP {
			return nil, fmt.Errorf("%s must allocate exactly %d SP (%d allocated).", name, c.StartingPilotSP, a.Total())
		}
		portrait := r.PostFormValue(prefix + "portrait")
		if portrait != "" && !validPortrait(portrait) {
			return nil, fmt.Errorf("%s: pick a portrait from the list.", name)
		}
		if portrait != "" && takenPortraits[portrait] {
			return nil, errors.New("Two of your pilots can't share a portrait.")
		}
		if portrait != "" {
			takenPortraits[portrait] = true
		}
		out = append(out, NewPilot{Name: name, Portrait: portrait, SP: a})
	}
	// A pilot sent without a portrait (not something the wizard does) is given a random free one.
	for i := range out {
		if out[i].Portrait == "" {
			out[i].Portrait = randomPortrait(takenPortraits)
			takenPortraits[out[i].Portrait] = true
		}
	}
	return out, nil
}

// livingPortraits are the portraits the living pilots in the list have picked.
func livingPortraits(pilots []NamedPilot) map[string]bool {
	taken := map[string]bool{}
	for _, p := range pilots {
		if !p.Fallen && p.Portrait != "" {
			taken[p.Portrait] = true
		}
	}
	return taken
}

// setPortrait changes a living pilot's portrait.
func (s *Server) setPortrait(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	portrait := r.PostFormValue("portrait")
	if !validPortrait(portrait) {
		http.Error(w, "Pick a portrait from the list.", http.StatusBadRequest)
		return
	}
	if err := s.store.SetPilotPortrait(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot"), portrait); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "No such living pilot.", http.StatusNotFound)
		return
	} else if errors.Is(err, ErrPortraitTaken) {
		http.Error(w, "Another pilot already has that portrait.", http.StatusBadRequest)
		return
	} else if err != nil {
		http.Error(w, "could not save the portrait", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteCampaign deletes the playthrough's progress in one campaign (see Store.DeleteRun). The browser asks the player
// to confirm first.
func (s *Server) deleteCampaign(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	if _, err := s.store.DeleteRun(r.Context(), pt.ID, c.Slug); err != nil {
		http.Error(w, "could not delete the campaign", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// earnSP records SP a pilot has earned (for instance from a scenario). It is unallocated until spent with allocateSP.
func (s *Server) earnSP(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	n, err := parseSP(r.PostFormValue("sp"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if n == 0 {
		http.Error(w, "Enter how much SP the pilot earned.", http.StatusBadRequest)
		return
	}
	if err := s.store.EarnSP(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot"), n); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "No such living pilot.", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "could not save the SP", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// allocateSP spends a pilot's unallocated SP across their four columns. The form holds the SP to add to each column,
// not their new totals; the server refuses more than the pilot has unallocated.
func (s *Server) allocateSP(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, "Allocate some SP first.", http.StatusBadRequest)
		return
	}
	if err := s.store.AllocateSP(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot"), a); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "No such living pilot.", http.StatusNotFound)
		return
	} else if errors.Is(err, ErrNotEnoughSP) {
		http.Error(w, "That is more SP than the pilot has unallocated.", http.StatusBadRequest)
		return
	} else if err != nil {
		http.Error(w, "could not save the SP", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
