package site

import (
	"embed"
	"encoding/json"
	"hash/fnv"
	"math/rand/v2"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// The named-pilot portraits are MechWarrior 5 art (see portraits/NOTICE.md), vendored by gen_portraits.go at 192x192
// with transparent backgrounds and embedded in the binary. A pilot picks one when they are created, and can change it
// later; two living pilots of a campaign never share one.
//
//go:embed portraits/*.png
var portraitsFS embed.FS

// Portrait is one choice in the portrait picker.
type Portrait struct {
	File  string `json:"file"`  // file name in portraits/, also its URL under /portraits/
	Group string `json:"group"` // the picker's section: Male, Female, Generic or Special
	Label string `json:"label"` // e.g. "Male, rank 2, no. 3"
}

var (
	portraitFileName = regexp.MustCompile(`^[a-z0-9._-]+\.png$`)
	portraitParts    = regexp.MustCompile(`^(male|female|generic)_rank(\d+)_(\d+)\.png$`)
	groupOrder       = map[string]int{"Male": 0, "Female": 1, "Generic": 2, "Special": 3}
)

// portraits lists every portrait, grouped (Male, Female, Generic, Special) and ordered by rank then number.
var portraits = sync.OnceValue(func() []Portrait {
	entries, err := portraitsFS.ReadDir("portraits")
	if err != nil {
		panic(err)
	}
	type keyed struct {
		p         Portrait
		rank, nth int
	}
	var all []keyed
	for _, e := range entries {
		file := e.Name()
		if !portraitFileName.MatchString(file) {
			continue
		}
		k := keyed{p: Portrait{File: file, Group: "Special"}}
		if m := portraitParts.FindStringSubmatch(file); m != nil {
			k.rank, _ = strconv.Atoi(m[2])
			k.nth, _ = strconv.Atoi(m[3])
			k.p.Group = strings.ToUpper(m[1][:1]) + m[1][1:]
			k.p.Label = k.p.Group + ", rank " + m[2] + ", no. " + m[3]
		} else {
			name := strings.TrimSuffix(strings.TrimSuffix(file, ".png"), "_pilot")
			k.p.Label = strings.ToUpper(name[:1]) + name[1:]
		}
		all = append(all, k)
	}
	sort.Slice(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if ga, gb := groupOrder[a.p.Group], groupOrder[b.p.Group]; ga != gb {
			return ga < gb
		}
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.nth != b.nth {
			return a.nth < b.nth
		}
		return a.p.File < b.p.File
	})
	out := make([]Portrait, len(all))
	for i, k := range all {
		out[i] = k.p
	}
	return out
})

var portraitSet = sync.OnceValue(func() map[string]bool {
	set := map[string]bool{}
	for _, p := range portraits() {
		set[p.File] = true
	}
	return set
})

// validPortrait reports whether file is one of the portraits.
func validPortrait(file string) bool { return portraitSet()[file] }

// portraitsJSON is the list as JSON, for the picker on the campaign page.
var portraitsJSON = sync.OnceValue(func() string {
	b, err := json.Marshal(portraits())
	if err != nil {
		panic(err)
	}
	return string(b)
})

func portraitURL(file string) string { return "/portraits/" + file }

// portraitOf is the portrait to show for a pilot: the one they picked, or for a pilot from before portraits existed a
// stable one worked out from their ID, so it doesn't change from one visit to the next.
func portraitOf(pilotID, stored string) string {
	if validPortrait(stored) {
		return stored
	}
	all := portraits()
	h := fnv.New32a()
	_, _ = h.Write([]byte(pilotID))
	return all[int(h.Sum32()%uint32(len(all)))].File
}

// randomPortrait picks a portrait at random from those not in taken (any, if somehow all are taken).
func randomPortrait(taken map[string]bool) string {
	all := portraits()
	var free []string
	for _, p := range all {
		if !taken[p.File] {
			free = append(free, p.File)
		}
	}
	if len(free) == 0 {
		return all[rand.IntN(len(all))].File
	}
	return free[rand.IntN(len(free))]
}

// portrait serves one of the embedded portraits. Only plain lowercase PNG names are accepted.
func (s *Server) portrait(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if !portraitFileName.MatchString(name) {
		s.notFound(w, r)
		return
	}
	b, err := portraitsFS.ReadFile("portraits/" + name)
	if err != nil {
		s.notFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(b)
}
