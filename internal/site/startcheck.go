package site

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

// A campaign can require its starting hangar to hold at least so many mechs, totalling at most so much BV (Intro.md
// front matter starting_min_mechs and starting_bv). BV is the unit's base BV, which is what a standard 4/5 pilot
// gives it: pilot skill never changes BV in this campaign. The wizard asks for this check as soon as a valid hangar
// link is pasted, and the server runs it again when the campaign is started, since a browser can't be trusted.

// startCheckUnit is one hangar unit as the starting check counts it.
type startCheckUnit struct {
	Name    string `json:"name"`
	BV      int    `json:"bv"`
	Mech    bool   `json:"mech"`
	Unknown bool   `json:"unknown"` // not in the unit catalog, so its BV can't be worked out
}

// startCheck is the result of checking a hangar against a campaign's starting conditions.
type startCheck struct {
	Units    []startCheckUnit `json:"units"`
	Mechs    int              `json:"mechs"`
	BV       int              `json:"bv"`
	MaxBV    int              `json:"maxBV"`    // 0 if the campaign sets no limit
	MinMechs int              `json:"minMechs"` // 0 if the campaign requires none
	Problems []string         `json:"problems"`
	OK       bool             `json:"ok"`
}

// checkStartingHangar counts a hangar's mechs and BV and lists what keeps it from meeting the campaign's starting
// conditions.
func checkStartingHangar(h HangarForce, c *Campaign) startCheck {
	res := startCheck{Units: []startCheckUnit{}, Problems: []string{}, MaxBV: c.StartingBV, MinMechs: c.StartingMinMechs}
	var unknown []string
	for _, hu := range h.Units {
		u, ok := unitByName(hu.Unit)
		if !ok {
			res.Units = append(res.Units, startCheckUnit{Name: hu.Unit, Unknown: true})
			unknown = append(unknown, hu.Unit)
			continue
		}
		cu := startCheckUnit{Name: displayName(u), BV: u.BV, Mech: u.Type == "Mek"}
		res.Units = append(res.Units, cu)
		res.BV += cu.BV
		if cu.Mech {
			res.Mechs++
		}
	}
	if len(unknown) > 0 {
		res.Problems = append(res.Problems, fmt.Sprintf("%s isn't in the unit catalog, so its BV is unknown.", strings.Join(unknown, ", ")))
	}
	if res.MinMechs > 0 && res.Mechs < res.MinMechs {
		res.Problems = append(res.Problems, fmt.Sprintf("You need at least %d mechs in your hangar (you have %d).", res.MinMechs, res.Mechs))
	}
	if res.MaxBV > 0 && res.BV > res.MaxBV {
		res.Problems = append(res.Problems, fmt.Sprintf("Your hangar totals %s BV, over the %s BV starting limit.", withCommas(res.BV), withCommas(res.MaxBV)))
	}
	res.OK = len(res.Problems) == 0
	return res
}

// hangarCheck reads the hangar behind a pasted link and checks it against a campaign's starting conditions, so the
// start wizard can say what is wrong before the player submits. It reads, never writes.
func (s *Server) hangarCheck(w http.ResponseWriter, r *http.Request) {
	seed := currentSeed(r)
	if seed == "" {
		http.Error(w, "Choose a seed first.", http.StatusBadRequest)
		return
	}
	c, ok := s.lib.Campaign(r.FormValue("campaign"))
	if !ok {
		http.Error(w, "unknown campaign", http.StatusNotFound)
		return
	}
	pt, found, err := s.store.Playthrough(r.Context(), playthroughID(seed))
	if err != nil {
		http.Error(w, "could not load playthrough", http.StatusInternalServerError)
		return
	}
	if !found || !pt.Configured() {
		http.Error(w, "Name your company first.", http.StatusBadRequest)
		return
	}
	link, err := parseHangar(r.FormValue("hangar"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h, err := s.mekbay.ReadHangar(r.Context(), link.Instance)
	if err != nil {
		log.Printf("read hangar for the start check: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(checkStartingHangar(h, c))
}

// withCommas formats a number with thousands separators, e.g. 6000 as "6,000".
func withCommas(n int) string {
	digits := fmt.Sprint(n)
	for i := len(digits) - 3; i > 0; i -= 3 {
		digits = digits[:i] + "," + digits[i:]
	}
	return digits
}
