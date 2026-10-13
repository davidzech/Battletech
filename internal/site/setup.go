package site

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Hangar is a parsed MekBay hangar link such as
// https://mekbay.com/?instance=01a12f9e-188f-7158-ad67-3fc9700ec733&sel=01a12f9d-a79b-717d-8c84-d5da73d2f9a5
type Hangar struct {
	URL      string // the link as pasted (trimmed)
	Instance string // required ?instance= id
	Sel      string // optional ?sel= id
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// parseHangar validates a MekBay hangar link: https, on mekbay.com, with an instance id.
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
	instance := strings.ToLower(q.Get("instance"))
	if !uuidPattern.MatchString(instance) {
		return Hangar{}, errors.New("That MekBay link has no valid instance id (?instance=…).")
	}
	sel := strings.ToLower(q.Get("sel"))
	if sel != "" && !uuidPattern.MatchString(sel) {
		return Hangar{}, errors.New("That MekBay link has an invalid selection id (&sel=…).")
	}
	return Hangar{URL: raw, Instance: instance, Sel: sel}, nil
}

// parsePlayers validates the human player count (1 to 4).
func parsePlayers(raw string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 || n > 4 {
		return 0, errors.New("Choose between 1 and 4 human players.")
	}
	return n, nil
}

// setCampaign saves the hangar link and player count for the visitor's playthrough.
func (s *Server) setCampaign(w http.ResponseWriter, r *http.Request) {
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
	hangar, err := parseHangar(r.PostFormValue("hangar"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	players, err := parsePlayers(r.PostFormValue("players"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id := playthroughID(seed)
	if _, err := s.store.EnsurePlaythrough(r.Context(), id); err != nil {
		http.Error(w, "could not save campaign", http.StatusInternalServerError)
		return
	}
	if err := s.store.SaveSetup(r.Context(), id, hangar, players); err != nil {
		http.Error(w, "could not save campaign", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setSeed validates a chosen seed, records its playthrough and stores the seed in a cookie. The
// reply says whether that playthrough already has its campaign setup, so the page knows whether
// to ask for the hangar link and player count.
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
