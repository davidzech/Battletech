package site

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/format"
	"github.com/davidzech/Battletech/internal/pilot"
	"github.com/davidzech/Battletech/internal/storage"
)

// pilotRequest checks what every endpoint that changes a campaign needs (see runRequest) and returns the playthrough and the campaign. It writes the error
// response itself and reports ok=false.
func (s *Server) pilotRequest(w http.ResponseWriter, r *http.Request) (pt storage.Playthrough, c *content.Campaign, ok bool) {
	pt, c, _, ok = s.runRequest(w, r)
	return pt, c, ok
}

// addPilot names a new named pilot for the campaign. They start with no SP: SP is earned, then allocated.
func (s *Server) addPilot(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	name, err := pilot.ParsePilotName(r.PostFormValue("name"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	portrait := r.PostFormValue("portrait")
	if portrait != "" && !pilot.ValidPortrait(portrait) {
		http.Error(w, "Pick a portrait from the list.", http.StatusBadRequest)
		return
	}
	if portrait == "" {
		existing, err := s.store.Pilots(r.Context(), pt.ID, c.Slug)
		if err != nil {
			http.Error(w, "could not load named pilots", http.StatusInternalServerError)
			return
		}
		portrait = pilot.RandomPortrait(livingPortraits(existing))
	}
	rank := c.RecruitRank() // every recruit joins at the campaign's recruit_rank (Refusal War: MechWarrior); it isn't chosen
	// A recruit starts with the campaign's starting SP (150 in Refusal War), spent as the player chose and the rest unallocated.
	start, err := parseSPAllocation(r, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if start.Total() > c.StartingPilotSP {
		http.Error(w, fmt.Sprintf("A new pilot has %d SP to allocate.", c.StartingPilotSP), http.StatusBadRequest)
		return
	}
	// Recruiting costs SP from the warchest (the campaign's recruit_cost): charged first, and given back if the pilot can't be added. Once charged, the rest
	// doesn't stop when the request does (the player closes the tab): the pilot is added, or the charge given back.
	paidCtx := context.WithoutCancel(r.Context())
	cost := c.RecruitCost
	// A free recruit (a mission outcome can grant one, e.g. a replacement for a pilot who died) is used up instead of paying.
	usedFree := false
	if cost > 0 {
		if ok, err := s.store.ConsumeFreeRecruit(r.Context(), pt.ID, c.Slug); err != nil {
			http.Error(w, "could not use the free recruit", http.StatusInternalServerError)
			return
		} else if ok {
			usedFree, cost = true, 0
		}
	}
	if cost > 0 {
		if _, err := s.store.SpendWarchest(r.Context(), pt.ID, c.Slug, cost); errors.Is(err, storage.ErrInsufficientFunds) {
			http.Error(w, fmt.Sprintf("Not enough in the warchest: recruiting a pilot costs %d SP.", cost), http.StatusConflict)
			return
		} else if err != nil {
			http.Error(w, "could not charge the warchest", http.StatusInternalServerError)
			return
		}
	}
	refund := func() {
		if usedFree {
			if err := s.store.RestoreFreeRecruit(paidCtx, pt.ID, c.Slug); err != nil {
				log.Printf("could not restore a free recruit: %v", err)
			}
		}
		if cost > 0 {
			if _, err := s.store.RefundWarchest(paidCtx, pt.ID, c.Slug, cost); err != nil {
				log.Printf("could not refund %d SP: %v", cost, err)
			}
		}
	}
	if _, err := s.store.AddPilotWithRank(paidCtx, pt.ID, c.Slug, name, portrait, rank, start, c.StartingPilotSP, c.PilotLimit()); errors.Is(err, pilot.ErrPilotLimit) {
		refund()
		http.Error(w, fmt.Sprintf("You can have at most %d named pilots at a time.", c.PilotLimit()), http.StatusBadRequest)
		return
	} else if errors.Is(err, storage.ErrNameTaken) {
		refund()
		http.Error(w, "Another pilot already has that name.", http.StatusBadRequest)
		return
	} else if errors.Is(err, pilot.ErrPortraitTaken) {
		refund()
		http.Error(w, "Another pilot already has that portrait.", http.StatusBadRequest)
		return
	} else if err != nil {
		refund()
		http.Error(w, "could not save the pilot", http.StatusInternalServerError)
		return
	}
	paid := fmt.Sprintf("for %d SP", cost)
	if usedFree {
		paid = "with a free recruit"
	} else if cost == 0 {
		paid = "for free"
	}
	text := fmt.Sprintf("Recruited %s %s %s", rank, name, paid)
	if a := allocationText(start); a != "" {
		text += " (" + a + ")"
	}
	s.logHistory(r, pt.ID, c.Slug, storage.HistoryPilot, -cost, strings.TrimSpace(text)+".")
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

// parseSPAllocation reads the four SP columns of a request (the fields gunnery, piloting, edge_tokens and edge_abilities after the prefix; an empty one is 0).
func parseSPAllocation(r *http.Request, prefix string) (a pilot.SPAllocation, err error) {
	for _, f := range []struct {
		field string
		dst   *int
	}{{"gunnery", &a.Gunnery}, {"piloting", &a.Piloting}, {"edge_tokens", &a.EdgeTokens}, {"edge_abilities", &a.EdgeAbilities}} {
		if *f.dst, err = parseSP(r.PostFormValue(prefix + f.field)); err != nil {
			return pilot.SPAllocation{}, err
		}
	}
	return a, nil
}

// pilotFallen records that a named pilot died. They are permanently removed from the campaign.
func (s *Server) pilotFallen(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	p, found := s.pilotByID(r, pt.ID, c.Slug, r.PostFormValue("pilot"))
	if err := s.store.MarkFallen(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot")); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "No such living pilot.", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "could not save", http.StatusInternalServerError)
		return
	}
	s.logHistory(r, pt.ID, c.Slug, storage.HistoryCorrections, 0, pilotLabel(p, found)+" was marked killed in action (Advanced Management).")
	w.WriteHeader(http.StatusNoContent)
}

// parseStartingPilots reads the pilots a campaign's start wizard submits (fields pilot0_name, pilot0_gunnery, ...):
// exactly the campaign's number of starting pilots, each with a name of their own, and every one allocating all of
// the campaign's starting SP, as the book has a pilot allocate SP the moment they earn it.
func parseStartingPilots(r *http.Request, c *content.Campaign) ([]pilot.NewPilot, error) {
	var out []pilot.NewPilot
	seen := map[string]bool{}
	takenPortraits := map[string]bool{}
	for i := 0; i < c.StartingNamedPilots; i++ {
		prefix := "pilot" + strconv.Itoa(i) + "_"
		name, err := pilot.ParsePilotName(r.PostFormValue(prefix + "name"))
		if err != nil {
			return nil, fmt.Errorf("Named pilot %d: %w", i+1, err)
		}
		if seen[strings.ToLower(name)] {
			return nil, errors.New("Two of your pilots can't share a name.")
		}
		seen[strings.ToLower(name)] = true
		a, err := parseSPAllocation(r, prefix)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if a.Total() != c.StartingPilotSP {
			return nil, fmt.Errorf("%s must allocate exactly %d SP (%d allocated).", name, c.StartingPilotSP, a.Total())
		}
		portrait := r.PostFormValue(prefix + "portrait")
		if portrait != "" && !pilot.ValidPortrait(portrait) {
			return nil, fmt.Errorf("%s: pick a portrait from the list.", name)
		}
		if portrait != "" && takenPortraits[portrait] {
			return nil, errors.New("Two of your pilots can't share a portrait.")
		}
		if portrait != "" {
			takenPortraits[portrait] = true
		}
		// A starting pilot who already reaches an Edge abilities level has to choose that many abilities now, all different.
		abilities := r.PostForm[prefix+"ability"]
		if want := pilot.EdgeAbilitiesLadder.At(a.EdgeAbilities).Level; len(abilities) != want {
			return nil, fmt.Errorf("%s must choose %d Edge abilities (%d chosen).", name, want, len(abilities))
		}
		chosen := map[string]bool{}
		for _, id := range abilities {
			if _, ok := pilot.AbilityByID(id); !ok {
				return nil, fmt.Errorf("%s: unknown Edge ability.", name)
			}
			if chosen[id] {
				return nil, fmt.Errorf("%s chose the same Edge ability twice.", name)
			}
			chosen[id] = true
		}
		out = append(out, pilot.NewPilot{Name: name, Portrait: portrait, SP: a, Abilities: abilities})
	}
	// A pilot sent without a portrait (not something the wizard does) is given a random free one.
	for i := range out {
		if out[i].Portrait == "" {
			out[i].Portrait = pilot.RandomPortrait(takenPortraits)
			takenPortraits[out[i].Portrait] = true
		}
	}
	return out, nil
}

// livingPortraits are the portraits the living pilots in the list have picked.
func livingPortraits(pilots []pilot.NamedPilot) map[string]bool {
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
	if !pilot.ValidPortrait(portrait) {
		http.Error(w, "Pick a portrait from the list.", http.StatusBadRequest)
		return
	}
	p, found := s.pilotByID(r, pt.ID, c.Slug, r.PostFormValue("pilot"))
	if err := s.store.SetPilotPortrait(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot"), portrait); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "No such living pilot.", http.StatusNotFound)
		return
	} else if errors.Is(err, pilot.ErrPortraitTaken) {
		http.Error(w, "Another pilot already has that portrait.", http.StatusBadRequest)
		return
	} else if err != nil {
		http.Error(w, "could not save the portrait", http.StatusInternalServerError)
		return
	}
	s.logHistory(r, pt.ID, c.Slug, storage.HistoryPilot, 0, pilotLabel(p, found)+portraitHistorySuffix)
	w.WriteHeader(http.StatusNoContent)
}

// deleteCampaign deletes the playthrough's progress in one campaign (see Store.DeleteRun). The browser asks the player
// to confirm first. What the campaign saved in MekBay goes too, in the background once the rows are gone: the hangar (only
// the site's own copy, never a force the player linked), its checkpoints' frozen copies, and every battle it recorded.
func (s *Server) deleteCampaign(w http.ResponseWriter, r *http.Request) {
	pt, c, run, ok := s.openRunRequest(w, r) // deleting is allowed during a battle: it removes the battle with everything else
	if !ok {
		return
	}
	// Listed before the rows that name them are deleted. A list that can't be read only leaves those behind in MekBay.
	var forces []string
	if run.Owned() && run.HangarInstance != "" {
		forces = append(forces, run.HangarInstance)
	}
	copies, err := s.store.CheckpointHangars(r.Context(), pt.ID, c.Slug)
	if err != nil {
		log.Printf("list the checkpoints' hangars to delete: %v", err)
	}
	forces = append(forces, copies...)
	operations, err := s.store.MekBayOperationIDs(r.Context(), pt.ID, c.Slug)
	if err != nil {
		log.Printf("list the battles to delete: %v", err)
	}
	if _, err := s.store.DeleteRun(r.Context(), pt.ID, c.Slug); err != nil {
		http.Error(w, "could not delete the campaign", http.StatusInternalServerError)
		return
	}
	s.cleanUpInBackground(currentSeed(r), forces, operations)
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
	a, err := parseSPAllocation(r, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
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
	p, found := s.pilotByID(r, pt.ID, c.Slug, r.PostFormValue("pilot"))
	if err := s.store.ApplySP(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot"), earned, a); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "No such living pilot.", http.StatusNotFound)
		return
	} else if errors.Is(err, storage.ErrNotEnoughSP) {
		http.Error(w, "That is more SP than the pilot has unallocated.", http.StatusBadRequest)
		return
	} else if err != nil {
		http.Error(w, "could not save the SP", http.StatusInternalServerError)
		return
	}
	var parts []string
	if earned > 0 {
		parts = append(parts, fmt.Sprintf("earned %d SP", earned))
	}
	if a.Total() > 0 {
		parts = append(parts, fmt.Sprintf("allocated %d SP (%s)", a.Total(), allocationText(a)))
	}
	s.logHistory(r, pt.ID, c.Slug, storage.HistoryPilot, 0, pilotLabel(p, found)+" "+strings.Join(parts, " and ")+".")
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
	alt := pilot.RandomPortrait(livingPortraits(existing))
	p, found := s.pilotByID(r, pt.ID, c.Slug, r.PostFormValue("pilot"))
	if err := s.store.RevivePilot(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot"), alt, c.PilotLimit()); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "No such pilot killed in action.", http.StatusNotFound)
		return
	} else if errors.Is(err, pilot.ErrPilotLimit) {
		http.Error(w, fmt.Sprintf("You already have %d living named pilots.", c.PilotLimit()), http.StatusBadRequest)
		return
	} else if err != nil {
		http.Error(w, "could not revive the pilot", http.StatusInternalServerError)
		return
	}
	s.logHistory(r, pt.ID, c.Slug, storage.HistoryCorrections, 0, pilotLabel(p, found)+" was revived (Advanced Management).")
	w.WriteHeader(http.StatusNoContent)
}

// deletePilot permanently removes a pilot, living or killed in action.
func (s *Server) deletePilot(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	p, found := s.pilotByID(r, pt.ID, c.Slug, r.PostFormValue("pilot"))
	if err := s.store.DeletePilot(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot")); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "No such pilot.", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "could not delete the pilot", http.StatusInternalServerError)
		return
	}
	s.logHistory(r, pt.ID, c.Slug, storage.HistoryCorrections, 0, pilotLabel(p, found)+" was deleted (Advanced Management).")
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
	p, found := s.pilotByID(r, pt.ID, c.Slug, r.PostFormValue("pilot"))
	if err := s.store.ResetPilotSP(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot")); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "No such pilot.", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "could not reset the pilot", http.StatusInternalServerError)
		return
	}
	s.logHistory(r, pt.ID, c.Slug, storage.HistoryCorrections, 0, pilotLabel(p, found)+"'s SP allocation was reset (Advanced Management).")
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
	p, found := s.pilotByID(r, pt.ID, c.Slug, r.PostFormValue("pilot"))
	err = s.store.SetPilotTotal(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot"), total, r.PostFormValue("reset") == "1")
	switch {
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "No such pilot.", http.StatusNotFound)
	case errors.Is(err, storage.ErrTotalBelowAllocated):
		http.Error(w, "That total is below the SP already allocated. Setting it resets the pilot's allocation.", http.StatusConflict)
	case err != nil:
		http.Error(w, "could not save the total", http.StatusInternalServerError)
	default:
		s.logHistory(r, pt.ID, c.Slug, storage.HistoryCorrections, 0, fmt.Sprintf("%s's total SP was set to %s (Advanced Management).", pilotLabel(p, found), format.Commas(total)))
		w.WriteHeader(http.StatusNoContent)
	}
}

// maxWarchest keeps a typo from setting an absurd amount of funds.
const maxWarchest = 100000000

// setWarchest overrides the force's funds (Advanced Management).
func (s *Server) setWarchest(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	funds, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("funds")))
	if err != nil || funds < 0 || funds > maxWarchest {
		http.Error(w, fmt.Sprintf("The funds must be a whole number from 0 to %d.", maxWarchest), http.StatusBadRequest)
		return
	}
	before, _, err := s.store.Run(r.Context(), pt.ID, c.Slug)
	if err != nil {
		http.Error(w, "could not load the campaign", http.StatusInternalServerError)
		return
	}
	if err := s.store.SetWarchest(r.Context(), pt.ID, c.Slug, funds); err != nil {
		http.Error(w, "could not save the funds", http.StatusInternalServerError)
		return
	}
	if funds != before.Warchest {
		s.logHistory(r, pt.ID, c.Slug, storage.HistoryCorrections, funds-before.Warchest, fmt.Sprintf("The warchest was set to %s SP (Advanced Management).", format.Commas(funds)))
	}
	w.WriteHeader(http.StatusNoContent)
}

// renamePilot changes a pilot's name, to correct a typo or a mix-up.
func (s *Server) renamePilot(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	name, err := pilot.ParsePilotName(r.PostFormValue("name"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p, found := s.pilotByID(r, pt.ID, c.Slug, r.PostFormValue("pilot"))
	switch err := s.store.RenamePilot(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot"), name); {
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "No such pilot.", http.StatusNotFound)
	case errors.Is(err, storage.ErrNameTaken):
		http.Error(w, "Another pilot already has that name.", http.StatusBadRequest)
	case err != nil:
		http.Error(w, "could not rename the pilot", http.StatusInternalServerError)
	default:
		if !found || p.Name != name {
			s.logHistory(r, pt.ID, c.Slug, storage.HistoryPilot, 0, fmt.Sprintf("%s was renamed %s.", pilotLabel(p, found), name))
		}
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
	if !pilot.ValidRank(c.Ranks, rank) {
		http.Error(w, "Pick a rank from the list.", http.StatusBadRequest)
		return
	}
	p, found := s.pilotByID(r, pt.ID, c.Slug, r.PostFormValue("pilot"))
	if err := s.store.SetPilotRank(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot"), rank); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "No such pilot.", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "could not save the rank", http.StatusInternalServerError)
		return
	}
	s.logHistory(r, pt.ID, c.Slug, storage.HistoryCorrections, 0, fmt.Sprintf("%s's rank was set to %s (Advanced Management).", pilotLabel(p, found), rank))
	w.WriteHeader(http.StatusNoContent)
}

// learnAbilities records the Edge abilities a pilot chooses (one "ability" field per choice, all of the pending ones at once).
func (s *Server) learnAbilities(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	p, found := s.pilotByID(r, pt.ID, c.Slug, r.PostFormValue("pilot"))
	err := s.store.LearnAbilities(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot"), r.PostForm["ability"])
	switch {
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "No such living pilot.", http.StatusNotFound)
	case errors.Is(err, storage.ErrNoPendingAbilities):
		http.Error(w, "This pilot has no new Edge ability to choose.", http.StatusBadRequest)
	case errors.Is(err, storage.ErrAbilityCount):
		http.Error(w, "Choose an Edge ability for each one the pilot has earned.", http.StatusBadRequest)
	case errors.Is(err, storage.ErrUnknownAbility):
		http.Error(w, "Pick the Edge abilities from the list.", http.StatusBadRequest)
	case errors.Is(err, storage.ErrAbilityLearned):
		http.Error(w, "Each Edge ability can be learned once.", http.StatusBadRequest)
	case err != nil:
		http.Error(w, "could not save the Edge abilities", http.StatusInternalServerError)
	default:
		s.logHistory(r, pt.ID, c.Slug, storage.HistoryPilot, 0, fmt.Sprintf("%s learned %s.", pilotLabel(p, found), abilityNames(r.PostForm["ability"])))
		w.WriteHeader(http.StatusNoContent)
	}
}

// resetEdgeAbilities forgets a pilot's learned Edge abilities (Advanced Management).
func (s *Server) resetEdgeAbilities(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	p, found := s.pilotByID(r, pt.ID, c.Slug, r.PostFormValue("pilot"))
	if err := s.store.ResetEdgeAbilities(r.Context(), pt.ID, c.Slug, r.PostFormValue("pilot")); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "No such pilot.", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "could not reset the Edge abilities", http.StatusInternalServerError)
		return
	}
	s.logHistory(r, pt.ID, c.Slug, storage.HistoryCorrections, 0, pilotLabel(p, found)+"'s Edge abilities were reset (Advanced Management).")
	w.WriteHeader(http.StatusNoContent)
}

// recruitStatus says whether the command can recruit a named pilot now, and the brief note shown under the Recruit button: why not (a full command, too little in
// the warchest), or what it costs.
func recruitStatus(c *content.Campaign, run storage.CampaignRun, alive int) (can bool, note string) {
	limit, cost, free := c.PilotLimit(), c.RecruitCost, run.FreeRecruits > 0
	can = alive < limit && (free || run.Warchest >= cost)
	switch {
	case alive >= limit:
		note = fmt.Sprintf("Your command is full: %d of %d pilots.", alive, limit)
	case free:
		note = "A replacement pilot is free: recruiting costs no SP."
	case run.Warchest < cost:
		note = fmt.Sprintf("Recruiting costs %s SP (warchest: %s).", format.Commas(cost), format.Commas(run.Warchest))
	case cost > 0:
		note = fmt.Sprintf("Recruiting costs %s SP.", format.Commas(cost))
	}
	return can, note
}
