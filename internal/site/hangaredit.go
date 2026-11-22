package site

import (
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/davidzech/Battletech/internal/content"
)

// What every endpoint that changes a campaign needs first, and how a paid change to the site's copy of the hangar is made.

// runRequest checks what every endpoint that changes a campaign needs: a same-origin POST from a visitor with a seed whose account is named and who has
// started the campaign the request names. It returns the playthrough, the campaign and the playthrough's run of it, answers the request itself on a problem and
// reports ok=false.
func (s *Server) runRequest(w http.ResponseWriter, r *http.Request) (pt Playthrough, c *content.Campaign, run CampaignRun, ok bool) {
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
func (s *Server) startedRun(w http.ResponseWriter, r *http.Request, seed string, c *content.Campaign) (pt Playthrough, run CampaignRun, ok bool) {
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
func (s *Server) hangarRequest(w http.ResponseWriter, r *http.Request) (pt Playthrough, c *content.Campaign, run CampaignRun, ok bool) {
	if pt, c, run, ok = s.runRequest(w, r); !ok {
		return
	}
	if !run.Owned() {
		http.Error(w, notOwnedMessage, http.StatusConflict)
		return pt, c, run, false
	}
	return pt, c, run, true
}

// payForHangarChange charges cost SP to the warchest and then makes the change in MekBay (change); if that fails the SP are refunded, so the player never pays
// for a change that wasn't made. It answers the request itself: {"cost", "warchest"} on success. what names the change for the log ("repair a unit").
func (s *Server) payForHangarChange(w http.ResponseWriter, r *http.Request, pt Playthrough, c *content.Campaign, run CampaignRun, cost int, what string, change func() error) {
	if err := s.store.SpendWarchest(r.Context(), pt.ID, c.Slug, cost); errors.Is(err, ErrInsufficientFunds) {
		http.Error(w, fmt.Sprintf("Not enough in the warchest: that costs %d SP and you have %d.", cost, run.Warchest), http.StatusConflict)
		return
	} else if err != nil {
		http.Error(w, "could not charge the warchest", http.StatusInternalServerError)
		return
	}
	if err := change(); err != nil {
		log.Printf("%s in MekBay: %v", what, err) // never log the owner UID
		if rerr := s.store.RefundWarchest(r.Context(), pt.ID, c.Slug, cost); rerr != nil {
			log.Printf("could not refund %d SP: %v", cost, rerr)
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"cost":%d,"warchest":%d}`, cost, run.Warchest-cost)
}
