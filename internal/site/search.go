package site

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// Searching the unit list to buy from. MekBay's catalog (units_index.json, here) is searched by the site itself: MekBay offers no embeddable search
// or way to hand a chosen unit back, but its data is public, and its search is simple (typed words matched, punctuation ignored, against a unit's
// chassis and model) and easy to match over about ten thousand units. Each campaign can also name a faction and an era (Intro.md
// availability_faction and availability_era); the search then offers only the units MekBay lists as available to that faction in that era,
// unless the player asks for everything. See availability.json, built by gen_units.go from MekBay's faction and era files.

//go:embed availability.json
var availabilityJSON []byte

var (
	availOnce sync.Once
	availSets map[string]map[string]bool
)

// availableUnits is the set of unit names available to a faction in an era, or nil if the pair isn't known (so no filter can apply).
func availableUnits(faction, era string) map[string]bool {
	availOnce.Do(func() {
		var raw map[string][]string
		if err := json.Unmarshal(availabilityJSON, &raw); err != nil {
			panic("availability.json: " + err.Error())
		}
		availSets = make(map[string]map[string]bool, len(raw))
		for k, names := range raw {
			set := make(map[string]bool, len(names))
			for _, n := range names {
				set[n] = true
			}
			availSets[k] = set
		}
	})
	return availSets[faction+"|"+era]
}

var (
	searchOnce sync.Once
	searchable []searchEntry
)

// searchEntry is a mech with the text it is matched on prepared once.
type searchEntry struct {
	unit  Unit
	key   string   // chassis, model and name, lowercased with everything but letters and digits removed
	words []string // the words of the chassis and model, lowercased, for "starts a word" ranking
}

// collapse lowercases text and drops everything but letters and digits, the way MekBay matches ("Mad Dog" finds "Mad-Dog", "madcat" finds "Mad Cat").
func collapse(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func buildSearch() {
	searchOnce.Do(func() {
		loadUnits()
		for _, u := range unitsByName {
			if u.Type != "Mek" {
				continue
			}
			text := u.Chassis + " " + u.Model
			e := searchEntry{unit: u, key: collapse(text + " " + u.Name)}
			e.words = strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
			searchable = append(searchable, e)
		}
		sort.Slice(searchable, func(i, j int) bool { return searchable[i].unit.Name < searchable[j].unit.Name })
	})
}

// searchResult is what a search returns: the units shown, how many matched in all, and what filter was applied.
type searchResult struct {
	Units   []hangarUnitView `json:"units"`
	Total   int              `json:"total"`
	Filter  searchFilter     `json:"filter"`
	Limited bool             `json:"limited"` // more matched than are shown
}

type searchFilter struct {
	Faction string `json:"faction"`
	Era     string `json:"era"`
	Active  bool   `json:"active"` // only units available to the faction in the era are listed
}

const maxSearchResults = 40

// searchUnits finds the mechs whose chassis or model contain every typed word, best matches first: those where each word starts a word of the
// chassis or model come before those where a word is only found inside one, then by name. An empty query finds nothing. available, when not nil,
// limits the results to those units.
func searchUnits(query string, available map[string]bool) (units []Unit, total int) {
	buildSearch()
	var tokens []string
	for _, t := range strings.Fields(query) {
		if c := collapse(t); c != "" {
			tokens = append(tokens, c)
		}
	}
	if len(tokens) == 0 {
		return nil, 0
	}
	type hit struct {
		u    Unit
		rank int
	}
	var hits []hit
	for _, e := range searchable {
		if available != nil && !available[e.unit.Name] {
			continue
		}
		rank := 0
		ok := true
		for _, t := range tokens {
			if !strings.Contains(e.key, t) {
				ok = false
				break
			}
			starts := false
			for _, w := range e.words {
				if strings.HasPrefix(w, t) {
					starts = true
					break
				}
			}
			if !starts {
				rank = 1
			}
		}
		if ok {
			hits = append(hits, hit{e.unit, rank})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].rank != hits[j].rank {
			return hits[i].rank < hits[j].rank
		}
		return hits[i].u.Name < hits[j].u.Name
	})
	total = len(hits)
	for i := 0; i < len(hits) && i < maxSearchResults; i++ {
		units = append(units, hits[i].u)
	}
	return units, total
}

// catalogUnitView shows a catalog unit as the same view the hangar uses, plus the MekBay unit ID the purchase needs.
func catalogUnitView(u Unit) hangarUnitView {
	v := hangarUnitView{MekBayURL: unitPageURL(u.Name), Unit: u.Name}
	v.Name, v.Model = unitNames(u)
	v.Class, v.Role, v.Tons, v.Icon, v.BV = u.Class, roleLabel(u), u.Tons, iconURL(u), u.BV
	return v
}

// searchUnitsHandler is GET /api/units/search?q=&campaign=&all=1. The list is public MekBay data, so it needs no seed. With a campaign that names a
// faction and an era the results are limited to what that faction fields in that era, unless all=1.
func (s *Server) searchUnitsHandler(w http.ResponseWriter, r *http.Request) {
	res := searchResult{Units: []hangarUnitView{}}
	var available map[string]bool
	if c, ok := s.lib.Campaign(r.URL.Query().Get("campaign")); ok && c.AvailabilityFaction != "" && c.AvailabilityEra != "" {
		res.Filter = searchFilter{Faction: c.AvailabilityFaction, Era: c.AvailabilityEra}
		if available = availableUnits(c.AvailabilityFaction, c.AvailabilityEra); available != nil && r.URL.Query().Get("all") != "1" {
			res.Filter.Active = true
		} else {
			available = nil
		}
	}
	found, total := searchUnits(strings.TrimSpace(r.URL.Query().Get("q")), available)
	for _, u := range found {
		res.Units = append(res.Units, catalogUnitView(u))
	}
	res.Total, res.Limited = total, total > len(found)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(res)
}
