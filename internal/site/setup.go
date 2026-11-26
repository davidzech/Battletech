package site

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/format"
	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/pilot"
	"github.com/davidzech/Battletech/internal/storage"
)

// parseAccountName validates the account's name: whatever the player likes to call their account.
func parseAccountName(raw string) (string, error) { return pilot.ParseName(raw, "account") }

// setAccount saves the account's name for the visitor's playthrough.
func (s *Server) setAccount(w http.ResponseWriter, r *http.Request) {
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
	name, err := parseAccountName(r.PostFormValue("account"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id := playthroughID(seed)
	if _, err := s.store.EnsurePlaythrough(r.Context(), id); err != nil {
		http.Error(w, "could not save account", http.StatusInternalServerError)
		return
	}
	if err := s.store.SetAccountName(r.Context(), id, name); err != nil {
		http.Error(w, "could not save account", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// startCampaign starts one of the campaigns for the visitor's playthrough, with that campaign's own MekBay hangar and
// its starting named pilots (see parseStartingPilots), all together. Posting again for a campaign that is already
// started is refused with 409.
func (s *Server) startCampaign(w http.ResponseWriter, r *http.Request) {
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
	c, ok := s.lib.Campaign(r.PostFormValue("campaign"))
	if !ok {
		http.Error(w, "unknown campaign", http.StatusNotFound)
		return
	}
	id := playthroughID(seed)
	pt, found, err := s.store.Playthrough(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load playthrough", http.StatusInternalServerError)
		return
	}
	if !found || !pt.Configured() {
		http.Error(w, "Name your account first.", http.StatusBadRequest)
		return
	}
	// The starting hangar is either built from mechs picked on the site (repeated `unit` fields) or copied from a MekBay link (`hangar`).
	units, err := parseStartingUnits(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var source mekbay.Hangar
	if units == nil {
		if source, err = mekbay.ParseHangarLink(r.PostFormValue("hangar")); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	_, started, err := s.store.Run(r.Context(), id, c.Slug)
	if err != nil {
		http.Error(w, "could not load campaign", http.StatusInternalServerError)
		return
	}
	if started {
		// A started campaign's hangar is never replaced from here: what has happened to it since (repairs, battles, checkpoints) would be lost. To start over,
		// delete the campaign (or roll it back to its start).
		http.Error(w, "This campaign has already been started.", http.StatusConflict)
		return
	}
	pilots, err := parseStartingPilots(r, c)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	forceName, err := content.ParseForceName(r.PostFormValue("force_name"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	players, err := parseStartingPlayers(r.PostFormValue("players"), c)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The hangar must meet the campaign's starting conditions (enough mechs, within the starting BV).
	var hangar mekbay.Hangar
	var made mekbay.HangarForce
	if units != nil {
		if check := checkStartingHangar(mekbay.DraftHangar(units), c); !check.OK {
			http.Error(w, strings.Join(check.Problems, " "), http.StatusBadRequest)
			return
		}
		if hangar, made, err = s.buildHangar(r.Context(), seed, forceName, units); err != nil {
			log.Printf("build the starting hangar for %s: %v", c.Slug, err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	} else if c.StartingBV > 0 || c.StartingMinMechs > 0 {
		h, err := s.mekbay.ReadHangar(r.Context(), source.Instance)
		if err != nil {
			log.Printf("read hangar to start %s: %v", c.Slug, err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if check := checkStartingHangar(h, c); !check.OK {
			http.Error(w, strings.Join(check.Problems, " "), http.StatusBadRequest)
			return
		}
	}
	if units == nil {
		if hangar, made, err = s.cloneHangar(r.Context(), seed, source); err != nil {
			log.Printf("clone hangar for %s: %v", c.Slug, err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}
	if err := s.store.StartRunWithPilots(r.Context(), id, c.Slug, hangar, pilots, c.StartingWarchest, players, forceName); errors.Is(err, storage.ErrAlreadyStarted) {
		http.Error(w, "This campaign has already been started.", http.StatusConflict)
		return
	} else if err != nil {
		http.Error(w, "could not start the campaign", http.StatusInternalServerError)
		return
	}
	s.cacheHangar(r.Context(), id, c.Slug, hangar.Instance, made) // the new hangar is the run's first cached one
	// Logged before the starting checkpoint is recorded, so rolling back to it keeps this line.
	names := make([]string, 0, len(pilots))
	for _, p := range pilots {
		names = append(names, p.Name)
	}
	s.logHistory(r, id, c.Slug, storage.HistoryCampaign, c.StartingWarchest, fmt.Sprintf("Started the campaign as %s, with the named pilots %s and a warchest of %s SP.", forceName, strings.Join(names, " and "), format.Commas(c.StartingWarchest)))
	// The first checkpoint: the campaign as it starts, which aborting the first mission rolls back to.
	if run, ok, err := s.store.Run(r.Context(), id, c.Slug); err == nil && ok {
		copyID, err := s.checkpointCopy(r.Context(), currentSeed(r), run)
		if err != nil {
			log.Printf("the starting checkpoint's hangar copy: %v", err)
		} else {
			s.recordCheckpoint(r.Context(), currentSeed(r), id, c.Slug, startCheckpointLabel, copyID)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseStartingPlayers reads how many players the campaign is played with: a whole number within the campaign's range (a campaign that allows only one
// count needs none sent). The count is only the Deploy wizard's default, which can be changed for each deploy.
func parseStartingPlayers(raw string, c *content.Campaign) (int, error) {
	lo, hi := playerRange(c)
	if lo == hi {
		return lo, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("Choose how many players, from %d to %d.", lo, hi)
	}
	return n, nil
}

// cloneHangar copies the force the player linked into a new, clean force owned by the seed's own MekBay account, and returns the run's hangar: the copy's
// link and instance, with the player's link kept as its source.
func (s *Server) cloneHangar(ctx context.Context, seed string, source mekbay.Hangar) (mekbay.Hangar, mekbay.HangarForce, error) {
	target := mekbay.NewID()
	got, err := s.mekbay.CloneHangar(ctx, mekbay.CloneRequest{OwnerUID: mekbay.AccountUID(seed, mekbay.SlotHangar), Source: source.Instance, Target: target})
	if err != nil {
		return mekbay.Hangar{}, mekbay.HangarForce{}, err
	}
	return mekbay.Hangar{URL: "https://mekbay.com/?instance=" + target, Instance: target, Source: source.URL}, got, nil
}

// setSeed validates a chosen seed, records its playthrough and stores the seed in a cookie. The
// reply says whether that playthrough's account is already named, so the page knows whether to
// ask for its name.
func (s *Server) setSeed(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	seed := r.PostFormValue("seed")
	if !validSeed(seed) {
		http.Error(w, "A seed is 8 to 32 lowercase letters and digits.", http.StatusBadRequest)
		return
	}
	id := playthroughID(seed)
	if _, err := s.store.EnsurePlaythrough(r.Context(), id); err != nil {
		http.Error(w, "could not save playthrough", http.StatusInternalServerError)
		return
	}
	pt, _, err := s.store.Playthrough(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load playthrough", http.StatusInternalServerError)
		return
	}
	setSeedCookie(w, r, seed)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"configured": pt.Configured()})
}
