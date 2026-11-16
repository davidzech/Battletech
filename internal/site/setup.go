package site

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
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

// companyName allows the kinds of names the lore uses ("Wolf's Dragoons", "1st Somerset Strikers", "Kell Hounds").
var companyName = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} '’.,&:!()\-]*$`)

// parseCompanyName validates and tidies a company name: 1 to 40 characters, with runs of spaces collapsed.
func parseCompanyName(raw string) (string, error) {
	name := strings.Join(strings.Fields(raw), " ")
	if n := utf8.RuneCountInString(name); n < 1 || n > 40 || !companyName.MatchString(name) {
		return "", errors.New("Name your company with 1 to 40 letters, numbers and spaces (apostrophes, hyphens and a few marks are fine).")
	}
	return name, nil
}

// parseOwnerUID validates the optional MekBay account UID (the id MekBay keeps in the owner's browser).
// An empty value is fine and means "don't change".
func parseOwnerUID(raw string) (string, error) {
	uid := strings.ToLower(strings.TrimSpace(raw))
	if uid != "" && !uuidPattern.MatchString(uid) {
		return "", errors.New("That doesn't look like a MekBay user ID (it should be a UUID like 01234567-89ab-cdef-0123-456789abcdef).")
	}
	return uid, nil
}

// setCompany saves the company's name and the optional MekBay owner UID for the visitor's playthrough.
func (s *Server) setCompany(w http.ResponseWriter, r *http.Request) {
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
	name, err := parseCompanyName(r.PostFormValue("company"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	owner, err := parseOwnerUID(r.PostFormValue("owner"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id := playthroughID(seed)
	if _, err := s.store.EnsurePlaythrough(r.Context(), id); err != nil {
		http.Error(w, "could not save company", http.StatusInternalServerError)
		return
	}
	if err := s.store.SetCompanyName(r.Context(), id, name); err != nil {
		http.Error(w, "could not save company", http.StatusInternalServerError)
		return
	}
	// Modify mode is optional: a UID turns it on, "clear_owner" turns it off, and leaving the field blank
	// keeps whatever is already saved (the UID is never sent back to the page to prefill it).
	if owner != "" || r.PostFormValue("clear_owner") == "1" {
		if err := s.store.SetOwnerUID(r.Context(), id, owner); err != nil {
			http.Error(w, "could not save company", http.StatusInternalServerError)
			return
		}
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
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
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
		http.Error(w, "Name your company first.", http.StatusBadRequest)
		return
	}
	hangar, err := parseHangar(r.PostFormValue("hangar"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, started, err := s.store.Run(r.Context(), id, c.Slug)
	if err != nil {
		http.Error(w, "could not load campaign", http.StatusInternalServerError)
		return
	}
	if started {
		if err := s.store.StartRun(r.Context(), id, c.Slug, hangar); err != nil {
			http.Error(w, "could not save the hangar link", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	pilots, err := parseStartingPilots(r, c)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The hangar must meet the campaign's starting conditions (enough mechs, within the starting BV).
	if c.StartingBV > 0 || c.StartingMinMechs > 0 {
		h, err := s.mekbay.ReadHangar(r.Context(), hangar.Instance)
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
	if err := s.store.StartRunWithPilots(r.Context(), id, c.Slug, hangar, pilots); errors.Is(err, ErrAlreadyStarted) {
		http.Error(w, "This campaign has already been started.", http.StatusConflict)
		return
	} else if err != nil {
		http.Error(w, "could not start the campaign", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setSeed validates a chosen seed, records its playthrough and stores the seed in a cookie. The
// reply says whether that playthrough's company is already named, so the page knows whether to
// ask for the company's details.
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
