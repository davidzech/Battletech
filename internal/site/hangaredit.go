package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/storage"
)

// What every endpoint that changes a campaign needs first, and how a change to the campaign's hangar is made.

// runRequest checks what every endpoint that changes a campaign needs: a same-origin POST from a visitor with a seed whose account is named and who has
// started the campaign the request names. It returns the playthrough, the campaign and the playthrough's run of it, answers the request itself on a problem and
// reports ok=false.
//
// While a battle is in progress the whole campaign is locked (battleLock): the players finish the operation (complete or abort it) first. The few endpoints that
// belong to the battle itself, or may run during one, use openRunRequest and check what they allow themselves.
func (s *Server) runRequest(w http.ResponseWriter, r *http.Request) (pt storage.Playthrough, c *content.Campaign, run storage.CampaignRun, ok bool) {
	if pt, c, run, ok = s.openRunRequest(w, r); !ok {
		return
	}
	return pt, c, run, s.battleLock(w, r, pt, c, "")
}

// battleLock refuses the request with 409 when a battle is in progress in the run, unless it is the battle of the scenario allowed (a scenario ID; "" allows
// none). It answers the request itself and returns false when it refuses.
func (s *Server) battleLock(w http.ResponseWriter, r *http.Request, pt storage.Playthrough, c *content.Campaign, allowed string) bool {
	sc, err := s.battleInProgress(r.Context(), pt.ID, c, true)
	if err != nil {
		http.Error(w, "could not load the battle", http.StatusInternalServerError)
		return false
	}
	if sc == nil || (allowed != "" && sc.ID == allowed) {
		return true
	}
	http.Error(w, battleLockMessage(c, sc), http.StatusConflict)
	return false
}

// battleInProgress is the scenario whose battle locks the campaign, or nil: the first with a battle recorded that the campaign still has and that isn't
// completed (a leftover row for one that is, or for a scenario the campaign no longer has, would lock it with no way out), or, with claims, the scenario a
// start in progress has claimed (see Store.ClaimBattle).
func (s *Server) battleInProgress(ctx context.Context, playthroughID string, c *content.Campaign, claims bool) (*content.Scenario, error) {
	ids, err := s.store.BattleScenarios(ctx, playthroughID, c.Slug)
	if err != nil {
		return nil, err
	}
	if claims {
		if id, ok, err := s.store.BattleClaim(ctx, playthroughID, c.Slug); err != nil {
			return nil, err
		} else if ok {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	completed, err := s.store.Completions(ctx, playthroughID, c.Slug)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if sc, ok := c.ScenarioByID(id); ok && !completed[sc.Slug] {
			return sc, nil
		}
	}
	return nil, nil
}

// battleLockMessage says why the campaign is locked: which mission is in progress, and how to unlock it.
func battleLockMessage(c *content.Campaign, sc *content.Scenario) string {
	return c.MissionName(*sc) + " is in progress. Complete or abort it before changing the campaign."
}

// hangarUnit finds the hangar's unit with this ID (the last one, should two share it).
func hangarUnit(h mekbay.HangarForce, id string) (hu mekbay.HangarUnit, found bool) {
	for _, u := range h.Units {
		if u.ID == id {
			hu, found = u, true
		}
	}
	return hu, found
}

// openRunRequest is runRequest without the battle lock.
func (s *Server) openRunRequest(w http.ResponseWriter, r *http.Request) (pt storage.Playthrough, c *content.Campaign, run storage.CampaignRun, ok bool) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)
		return
	}
	seed := currentSeed(r)
	if seed == "" {
		http.Error(w, "Choose a seed first.", http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	c, found := s.lib.Campaign(r.PostFormValue("campaign"))
	if !found {
		http.Error(w, "unknown campaign", http.StatusNotFound)
		return
	}
	pt, run, ok = s.startedRun(w, r, seed, c)
	return pt, c, run, ok
}

// startedRun loads the seed's account, which must be named, and its run of the campaign, which must be started: what every campaign endpoint needs. It
// answers the request itself and reports ok=false.
func (s *Server) startedRun(w http.ResponseWriter, r *http.Request, seed string, c *content.Campaign) (pt storage.Playthrough, run storage.CampaignRun, ok bool) {
	pt, found, err := s.store.Playthrough(r.Context(), playthroughID(seed))
	if err != nil {
		http.Error(w, "could not load playthrough", http.StatusInternalServerError)
		return
	}
	if !found || !pt.Configured() {
		http.Error(w, "Name your account first.", http.StatusBadRequest)
		return
	}
	run, started, err := s.store.Run(r.Context(), pt.ID, c.Slug)
	if err != nil {
		http.Error(w, "could not load campaign", http.StatusInternalServerError)
		return
	}
	if !started {
		http.Error(w, "Start this campaign first.", http.StatusBadRequest)
		return
	}
	return pt, run, true
}

// hangarError is a change to the hangar refused for what the hangar holds (the unit is gone, has nothing to repair, can't take that variant), with the status it
// answers. A change is checked against the hangar as it is inside the transaction that makes it, so the check still holds when it is made.
type hangarError struct {
	status int
	msg    string
}

func (e hangarError) Error() string { return e.msg }

// refused is a change the edits in package mekbay turned down (mekbay.ErrNoChange: it has already been made) as a hangarError.
func refused(err error) error {
	if err == nil {
		return nil
	}
	return hangarError{http.StatusConflict, err.Error()}
}

// parseHangarMap reads a hangar being edited (the decoded JSON of its saved data) as the site reads a hangar.
func parseHangarMap(force map[string]any) (mekbay.HangarForce, error) {
	b, err := json.Marshal(force)
	if err != nil {
		return mekbay.HangarForce{}, err
	}
	return mekbay.ParseForce(b)
}

// noHangarMessage is the answer for a campaign whose hangar the import couldn't bring over from MekBay (storage.ErrNoHangar): it can't be used.
const noHangarMessage = "This campaign's hangar couldn't be brought over from MekBay, so it can't be used. Delete the campaign (Danger zone) and start it again."

// hangarLoadFailed answers a request whose campaign hangar couldn't be loaded (what says for what, for the log): a campaign without a hangar says so (409), one
// not started is asked to start (400), anything else is a 500.
func hangarLoadFailed(w http.ResponseWriter, err error, what string) {
	switch {
	case errors.Is(err, storage.ErrNoHangar):
		http.Error(w, noHangarMessage, http.StatusConflict)
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "Start this campaign first.", http.StatusBadRequest)
	default:
		log.Printf("load the hangar %s: %v", what, err)
		http.Error(w, "could not load your hangar", http.StatusInternalServerError)
	}
}

// hangarGuard is the battle guard of an edit to a campaign's hangar (see storage.BattleGuard): every battle locks it, but one of a scenario the campaign no
// longer has (as battleInProgress), and the claim of the scenario allowed ("" allows none).
func hangarGuard(c *content.Campaign, allowed string) storage.BattleGuard {
	return storage.BattleGuard{AllowClaim: allowed, Counts: func(id string) bool { _, ok := c.ScenarioByID(id); return ok }}
}

// battleRefusal is the battle lock's message for an edit refused inside its transaction because a battle started after the request checked the lock.
func battleRefusal(c *content.Campaign, e storage.BattleInProgressError) string {
	if sc, ok := c.ScenarioByID(e.Scenario); ok {
		return battleLockMessage(c, sc)
	}
	return "A battle is in progress. Complete or abort it before changing the campaign."
}

// loadHangar reads a campaign run's hangar from the database.
func (s *Server) loadHangar(ctx context.Context, playthroughID, campaign string) (mekbay.HangarForce, error) {
	raw, err := s.store.Hangar(ctx, playthroughID, campaign)
	if err != nil {
		return mekbay.HangarForce{}, err
	}
	return mekbay.ParseForce(raw)
}

// hangarChange is one change to a run's hangar: it gets the hangar to change (force, the decoded JSON of its saved data) and the same hangar as the site reads it
// (h, to check the change against), changes force, and returns the SP it moves into the warchest (negative for a charge) and its history line. A hangarError
// refuses it with that status and message.
type hangarChange func(force map[string]any, h mekbay.HangarForce) (sp int, history string, err error)

// changeHangar makes a change to a run's hangar and its warchest together, in one transaction (Store.EditHangarGuarded), so a change that is refused, or that
// the warchest can't cover, changes nothing and charges nothing. Changes made at once, by this server or another sharing the database, take turns, each starting
// from the hangar the last one saved. A battle started after the request passed the battle lock refuses it inside the transaction, with the lock's message. The
// change's line is logged once it is made. It answers the request itself on a problem and reports ok=false; on success it
// returns the warchest the change left (not the run loaded at the start of the request, which another request may have changed since) and the SP it moved.
func (s *Server) changeHangar(w http.ResponseWriter, r *http.Request, pt storage.Playthrough, c *content.Campaign, change hangarChange) (warchest, sp int, ok bool) {
	var history string
	warchest, err := s.store.EditHangarGuarded(r.Context(), pt.ID, c.Slug, hangarGuard(c, ""), func(force map[string]any) (int, error) {
		h, err := parseHangarMap(force)
		if err != nil {
			return 0, err
		}
		sp, history, err = change(force, h)
		return sp, err
	})
	var refusal hangarError
	var battle storage.BattleInProgressError
	switch {
	case errors.As(err, &refusal):
		http.Error(w, refusal.msg, refusal.status)
	case errors.As(err, &battle):
		http.Error(w, battleRefusal(c, battle), http.StatusConflict)
	case errors.Is(err, storage.ErrInsufficientFunds):
		http.Error(w, fmt.Sprintf("Not enough in the warchest: that costs %d SP and you have %d.", -sp, warchest), http.StatusConflict)
	case errors.Is(err, storage.ErrNoHangar):
		http.Error(w, noHangarMessage, http.StatusConflict)
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "Start this campaign first.", http.StatusBadRequest)
	case err != nil:
		log.Printf("change the hangar: %v", err)
		http.Error(w, "could not save the change to your hangar", http.StatusInternalServerError)
	default:
		s.logHistory(r, pt.ID, c.Slug, storage.HistoryHangar, sp, history)
		return warchest, sp, true
	}
	return warchest, sp, false
}

// writeCharge answers a paid change to the hangar: {"cost", "warchest"}.
func writeCharge(w http.ResponseWriter, cost, warchest int) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"cost":%d,"warchest":%d}`, cost, warchest)
}
