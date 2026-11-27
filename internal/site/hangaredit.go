package site

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/storage"
)

// What every endpoint that changes a campaign needs first, and how a paid change to the site's copy of the hangar is made.

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

// hangarRequest is runRequest for a change to the hangar: the run's hangar must be a copy the site owns, since MekBay only takes the owner's saves.
func (s *Server) hangarRequest(w http.ResponseWriter, r *http.Request) (pt storage.Playthrough, c *content.Campaign, run storage.CampaignRun, ok bool) {
	if pt, c, run, ok = s.runRequest(w, r); !ok {
		return
	}
	if !run.Owned() {
		http.Error(w, notOwnedMessage, http.StatusConflict)
		return pt, c, run, false
	}
	return pt, c, run, true
}

// errHangarBusy is lockHangar giving up on a claim another server holds.
var errHangarBusy = errors.New("Another change to your hangar is still being saved. Try again in a moment.")

// hangarClaimWait is how long lockHangar waits for another server's claim on a hangar before giving up, and hangarClaimPoll how often it tries again.
var hangarClaimWait, hangarClaimPoll = 15 * time.Second, 200 * time.Millisecond

// lockHangar locks a campaign run's hangar for one MekBay edit and returns the function that unlocks it. A MekBay force edit is read-modify-save, so two at
// once would both start from the same force and the later save would undo the earlier one, with both paid for. One at a time, the second starts from what the
// first saved, and an edit that then has nothing left to do (the same repair clicked twice) fails with mekbay.ErrNoChange and is refunded.
//
// Edits made by this process queue on an in-process mutex first (keyedMutex), so only one of them at a time asks the database; the database claim
// (Store.ClaimHangarEdit) then keeps out edits made by other servers sharing it. While another server holds the claim this tries again every hangarClaimPoll
// for up to hangarClaimWait, then fails with errHangarBusy. The claim is released even if the request has stopped.
func (s *Server) lockHangar(ctx context.Context, playthroughID, campaign string) (unlock func(), err error) {
	unlockLocal := s.hangarEdits.lock(playthroughID + "/" + campaign)
	deadline := time.Now().Add(hangarClaimWait)
	for {
		token, ok, err := s.store.ClaimHangarEdit(ctx, playthroughID, campaign)
		if err != nil {
			unlockLocal()
			return nil, fmt.Errorf("claim the hangar: %w", err)
		}
		if ok {
			return func() {
				if err := s.store.ReleaseHangarEdit(context.WithoutCancel(ctx), playthroughID, campaign, token); err != nil {
					log.Printf("release the hangar claim: %v", err) // it expires after storage.HangarClaimTTL
				}
				unlockLocal()
			}, nil
		}
		if time.Now().After(deadline) {
			unlockLocal()
			return nil, errHangarBusy
		}
		select {
		case <-ctx.Done():
			unlockLocal()
			return nil, ctx.Err()
		case <-time.After(hangarClaimPoll):
		}
	}
}

// lockHangarRequest is lockHangar for a request: it answers the request itself when the hangar can't be locked (409 while another server's edit holds it)
// and reports ok=false.
func (s *Server) lockHangarRequest(w http.ResponseWriter, r *http.Request, pt storage.Playthrough, c *content.Campaign) (unlock func(), ok bool) {
	unlock, err := s.lockHangar(r.Context(), pt.ID, c.Slug)
	if errors.Is(err, errHangarBusy) {
		http.Error(w, err.Error(), http.StatusConflict)
		return nil, false
	} else if err != nil {
		log.Printf("lock the hangar: %v", err)
		http.Error(w, "could not lock the hangar", http.StatusInternalServerError)
		return nil, false
	}
	return unlock, true
}

// keyedMutex is one mutex per key, made when first locked and forgotten when nobody holds or waits for it. The server keeps one per campaign run for the
// edits to its hangar (see lockHangar).
type keyedMutex struct {
	mu    sync.Mutex
	locks map[string]*keyedLock
}

type keyedLock struct {
	mu      sync.Mutex
	holders int // goroutines holding or waiting for mu
}

// lock locks the key's mutex and returns the function that unlocks it.
func (k *keyedMutex) lock(key string) (unlock func()) {
	k.mu.Lock()
	if k.locks == nil {
		k.locks = map[string]*keyedLock{}
	}
	l := k.locks[key]
	if l == nil {
		l = &keyedLock{}
		k.locks[key] = l
	}
	l.holders++
	k.mu.Unlock()
	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		k.mu.Lock()
		if l.holders--; l.holders == 0 {
			delete(k.locks, key)
		}
		k.mu.Unlock()
	}
}

// payForHangarChange charges cost SP to the warchest and then makes the change in MekBay (change); if that fails the SP are refunded, so the player never pays
// for a change that wasn't made. The refund doesn't stop when the request does: a player who closes the tab during the MekBay call would otherwise lose the SP.
// It answers the request itself: {"cost", "warchest"} on success, the warchest being what the charge left (not the run loaded at the start of the request,
// which another request may have changed since). what names the change for the log ("repair a unit").
//
// change returns the hangar force as MekBay saved it, which becomes the cached copy, so the page that reloads next paints the change at once instead of the
// hangar from before it until a live read catches up.
//
// The charge and the change run under the run's hangar lock (lockHangar). A change that had already been made (mekbay.ErrNoChange) is refunded like any
// failed change, and answered 409.
func (s *Server) payForHangarChange(w http.ResponseWriter, r *http.Request, pt storage.Playthrough, c *content.Campaign, run storage.CampaignRun, cost int, what, history string, change func() (mekbay.HangarForce, error)) {
	unlock, ok := s.lockHangarRequest(w, r, pt, c) // one edit of the run's hangar at a time
	if !ok {
		return
	}
	defer unlock()
	left, err := s.store.SpendWarchest(r.Context(), pt.ID, c.Slug, cost)
	if errors.Is(err, storage.ErrInsufficientFunds) {
		http.Error(w, fmt.Sprintf("Not enough in the warchest: that costs %d SP and you have %d.", cost, left), http.StatusConflict)
		return
	} else if err != nil {
		http.Error(w, "could not charge the warchest", http.StatusInternalServerError)
		return
	}
	h, err := change()
	if err != nil {
		log.Printf("%s in MekBay: %v", what, err) // never log the owner UID
		if _, rerr := s.store.RefundWarchest(context.WithoutCancel(r.Context()), pt.ID, c.Slug, cost); rerr != nil {
			log.Printf("could not refund %d SP: %v", cost, rerr)
		}
		status := http.StatusBadGateway
		if errors.Is(err, mekbay.ErrNoChange) {
			status = http.StatusConflict // nothing was wrong with MekBay: the change had already been made
		}
		http.Error(w, err.Error(), status)
		return
	}
	s.cacheHangar(context.WithoutCancel(r.Context()), pt.ID, c.Slug, run.HangarInstance, h)
	s.logHistory(r, pt.ID, c.Slug, storage.HistoryHangar, -cost, history)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"cost":%d,"warchest":%d}`, cost, left)
}
