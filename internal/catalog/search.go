package catalog

import (
	_ "embed"
	"sort"
	"strings"
	"sync"
	"unicode"
)

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

const maxSearchResults = 40

// Search finds the mechs whose chassis or model contain every typed word, best matches first: those where each word starts a word of the
// chassis or model come before those where a word is only found inside one, then by name. An empty query finds nothing. available, when not nil,
// limits the results to those units.
func Search(query string, available map[string]bool) (units []Unit, total int) {
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
