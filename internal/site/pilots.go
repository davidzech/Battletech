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
	rank := r.PostFormValue("rank")
	if rank != "" && !validRank(c.Ranks, rank) {
		http.Error(w, "Pick a rank from the list.", http.StatusBadRequest)
		return
	}
	// A recruit starts with the campaign's starting SP (150 in Refusal War), spent as the player chose and the rest unallocated.
	var start SPAllocation
	for field, dst := range map[string]*int{
		"gunnery": &start.Gunnery, "piloting": &start.Piloting, "edge_tokens": &start.EdgeTokens, "edge_abilities": &start.EdgeAbilities,
	} {
		n, err := parseSP(r.PostFormValue(field))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		*dst = n
	}
	if start.Total() > c.StartingPilotSP {
		http.Error(w, fmt.Sprintf("A new pilot has %d SP to allocate.", c.StartingPilotSP), http.StatusBadRequest)
		return
	}
	if _, err := s.store.AddPilotWithRank(r.Context(), pt.ID, c.Slug, name, portrait, rank, start, c.StartingPilotSP, maxNamedPilots); errors.Is(err, ErrPilotLimit) {
		http.Error(w, fmt.Sprintf("You can have at most %d named pilots at a time.", maxNamedPilots), http.StatusBadRequest)
		return
	} else if errors.Is(err, ErrNameTaken) {
		http.Error(w, "Another pilot already has that name.", http.StatusBadRequest)
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
// not their new totals, and optionally `earned`, SP to add to the pilot's total in the same step (the Manage SP dialog
// stages it, then commits it together with the allocation); the server refuses more than the pilot would then have
// unallocated.
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
	earned, err := parseSP(r.PostFormValue("earned"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if a.Total() == 0 && earned == 0 {
		http.Error(w, "Allocate some SP first.", http.StatusBadRequest)
		return
	}
	if err := s.store.ApplySP(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot"), earned, a); errors.Is(err, sql.ErrNoRows) {
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

// revivePilot brings a pilot marked killed in action back, to correct a misclick.
func (s *Server) revivePilot(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	existing, err := s.store.Pilots(r.Context(), pt.ID, c.Slug)
	if err != nil {
		http.Error(w, "could not load named pilots", http.StatusInternalServerError)
		return
	}
	alt := randomPortrait(livingPortraits(existing))
	if err := s.store.RevivePilot(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot"), alt, maxNamedPilots); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "No such pilot killed in action.", http.StatusNotFound)
		return
	} else if errors.Is(err, ErrPilotLimit) {
		http.Error(w, fmt.Sprintf("You already have %d living named pilots.", maxNamedPilots), http.StatusBadRequest)
		return
	} else if err != nil {
		http.Error(w, "could not revive the pilot", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deletePilot permanently removes a pilot, living or killed in action.
func (s *Server) deletePilot(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	if err := s.store.DeletePilot(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot")); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "No such pilot.", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "could not delete the pilot", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// maxTotalSP keeps a typo from setting an absurd total.
const maxTotalSP = 1000000

// resetPilotSP is the factory reset for a pilot's abilities (see Store.ResetPilotSP).
func (s *Server) resetPilotSP(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	if err := s.store.ResetPilotSP(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot")); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "No such pilot.", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "could not reset the pilot", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setPilotTotal overrides a pilot's total SP. A total below what is allocated is refused with 409 unless the form says
// reset=1, which the browser only sends after the player has confirmed that the allocation will be reset.
func (s *Server) setPilotTotal(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	raw := strings.TrimSpace(r.PostFormValue("total"))
	total, err := strconv.Atoi(raw)
	if err != nil || total < 0 || total > maxTotalSP {
		http.Error(w, fmt.Sprintf("The total must be a whole number from 0 to %d.", maxTotalSP), http.StatusBadRequest)
		return
	}
	err = s.store.SetPilotTotal(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot"), total, r.PostFormValue("reset") == "1")
	switch {
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "No such pilot.", http.StatusNotFound)
	case errors.Is(err, ErrTotalBelowAllocated):
		http.Error(w, "That total is below the SP already allocated. Setting it resets the pilot's allocation.", http.StatusConflict)
	case err != nil:
		http.Error(w, "could not save the total", http.StatusInternalServerError)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// renamePilot changes a pilot's name, to correct a typo or a mix-up.
func (s *Server) renamePilot(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	name, err := parsePilotName(r.PostFormValue("name"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	switch err := s.store.RenamePilot(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot"), name); {
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "No such pilot.", http.StatusNotFound)
	case errors.Is(err, ErrNameTaken):
		http.Error(w, "Another pilot already has that name.", http.StatusBadRequest)
	case err != nil:
		http.Error(w, "could not rename the pilot", http.StatusInternalServerError)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// setRank sets a pilot's cosmetic rank, which must be one of the campaign's ranks.
func (s *Server) setRank(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	rank := r.PostFormValue("rank")
	if !validRank(c.Ranks, rank) {
		http.Error(w, "Pick a rank from the list.", http.StatusBadRequest)
		return
	}
	if err := s.store.SetPilotRank(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot"), rank); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "No such pilot.", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "could not save the rank", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// learnAbilities records the Edge abilities a pilot chooses (one "ability" field per choice, all of the pending ones at once).
func (s *Server) learnAbilities(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	err := s.store.LearnAbilities(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot"), r.PostForm["ability"])
	switch {
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "No such living pilot.", http.StatusNotFound)
	case errors.Is(err, ErrNoPendingAbilities):
		http.Error(w, "This pilot has no new Edge ability to choose.", http.StatusBadRequest)
	case errors.Is(err, ErrAbilityCount):
		http.Error(w, "Choose an Edge ability for each one the pilot has earned.", http.StatusBadRequest)
	case errors.Is(err, ErrUnknownAbility):
		http.Error(w, "Pick the Edge abilities from the list.", http.StatusBadRequest)
	case errors.Is(err, ErrAbilityLearned):
		http.Error(w, "Each Edge ability can be learned once.", http.StatusBadRequest)
	case err != nil:
		http.Error(w, "could not save the Edge abilities", http.StatusInternalServerError)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// resetEdgeAbilities forgets a pilot's learned Edge abilities (Advanced Management).
func (s *Server) resetEdgeAbilities(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	if err := s.store.ResetEdgeAbilities(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot")); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "No such pilot.", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "could not reset the Edge abilities", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
