package site

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/davidzech/Battletech/internal/pilot"
)

// Hangar is a MekBay hangar link reduced to what the site needs: the force's instance ID. Whatever else a pasted link
// carries (the selected unit, an operation, tracking parameters, enemy forces) is dropped.
type Hangar struct {
	URL      string // the canonical link, https://mekbay.com/?instance=<id>
	Instance string // the hangar force's instance ID
	// Source, on a run's hangar, is the link the player gave. URL and Instance are then the site's own copy of that force (see clone.go), and parseHangar,
	// which reads what the player pasted, leaves it empty.
	Source string
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// friendlyInstance picks our own force out of a MekBay "instance" value, which lists forces separated by commas, the
// hostile ones prefixed "enemy:" (instance=<mine>,enemy:<theirs>). It is the first entry that isn't hostile, or "" if
// there is none or it isn't a valid ID.
func friendlyInstance(list string) string {
	for _, entry := range strings.Split(list, ",") {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if strings.HasPrefix(entry, "enemy:") {
			continue
		}
		if uuidPattern.MatchString(entry) {
			return entry
		}
		return ""
	}
	return ""
}

// parseHangar accepts any MekBay link to a hangar and reduces it to https://mekbay.com/?instance=<id>. The link must be
// https, on mekbay.com, with an instance ID. Extra parameters are harmless and are dropped, the unit selection
// (&sel=) among them; a list of forces ("<mine>,enemy:<theirs>") gives its friendly force.
func parseHangar(raw string) (Hangar, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	host := ""
	if err == nil {
		host = strings.ToLower(u.Hostname())
	}
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || (host != "mekbay.com" && host != "www.mekbay.com") {
		return Hangar{}, errors.New("Paste a MekBay hangar link, like https://mekbay.com/?instance=…")
	}
	q := u.Query()
	instance := friendlyInstance(q.Get("instance"))
	if instance == "" {
		if q.Get("operation") != "" && q.Get("instance") == "" {
			return Hangar{}, errors.New("That is an operation link. Paste the link to your hangar instead (it has ?instance=…).")
		}
		return Hangar{}, errors.New("That MekBay link has no valid instance id (?instance=…).")
	}
	return Hangar{URL: "https://mekbay.com/?instance=" + instance, Instance: instance}, nil
}

// parseAccountName validates the account's name: whatever the player likes to call their account.
func parseAccountName(raw string) (string, error) { return pilot.ParseName(raw, "account") }

// parseForceName validates a campaign's force name, e.g. "Wolf's Dragoons": each campaign's force has its own, to suit that campaign's theme.
func parseForceName(raw string) (string, error) { return pilot.ParseName(raw, "force") }

// setAccount saves the account's name and the players' optional MekBay user IDs for the visitor's playthrough.
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
// started just changes its hangar link.
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
	var source Hangar
	if units == nil {
		if source, err = parseHangar(r.PostFormValue("hangar")); err != nil {
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
	forceName, err := parseForceName(r.PostFormValue("force_name"))
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
	var hangar Hangar
	var made HangarForce
	if units != nil {
		if check := checkStartingHangar(draftHangar(units), c); !check.OK {
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
	if err := s.store.StartRunWithPilots(r.Context(), id, c.Slug, hangar, pilots, c.StartingWarchest, players, forceName); errors.Is(err, ErrAlreadyStarted) {
		http.Error(w, "This campaign has already been started.", http.StatusConflict)
		return
	} else if err != nil {
		http.Error(w, "could not start the campaign", http.StatusInternalServerError)
		return
	}
	s.cacheHangar(r.Context(), id, c.Slug, hangar.Instance, made) // the new hangar is the run's first cached one
	// The first checkpoint: the campaign as it starts, which aborting the first mission rolls back to.
	if run, ok, err := s.store.Run(r.Context(), id, c.Slug); err == nil && ok {
		copyID, err := s.checkpointCopy(r.Context(), currentSeed(r), run)
		if err != nil {
			log.Printf("the starting checkpoint's hangar copy: %v", err)
		} else {
			s.recordCheckpoint(r.Context(), currentSeed(r), id, c.Slug, "Campaign started", copyID)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseStartingPlayers reads how many players the campaign is played with: a whole number within the campaign's range (a campaign that allows only one
// count needs none sent). The count is only the Deploy wizard's default, which can be changed for each deploy.
func parseStartingPlayers(raw string, c *Campaign) (int, error) {
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
func (s *Server) cloneHangar(ctx context.Context, seed string, source Hangar) (Hangar, HangarForce, error) {
	target := newID()
	got, err := s.mekbay.CloneHangar(ctx, CloneRequest{OwnerUID: accountUID(seed, SlotHangar), Source: source.Instance, Target: target})
	if err != nil {
		return Hangar{}, HangarForce{}, err
	}
	return Hangar{URL: "https://mekbay.com/?instance=" + target, Instance: target, Source: source.URL}, got, nil
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
