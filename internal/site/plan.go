package site

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// The deployment plan: which mechs and pilots each player takes on a sortie. The Deploy wizard steps through the players one at a time, each choosing from what
// the earlier ones left, and ends with a review of the whole plan. This endpoint checks that plan on the server and describes it back. It does not touch MekBay yet:
// building each player's force in their own MekBay account, and the OpFor in the OpFor's, comes next, and the old single-player deploy (POST /api/mekbay/deploy)
// is still in the code but is not used by the wizard.

// playerRange is how many players a campaign can be deployed with (Intro.md min_players and max_players); a campaign that doesn't say is a one-player campaign.
func playerRange(c *Campaign) (lo, hi int) {
	lo, hi = c.MinPlayers, c.MaxPlayers
	if hi <= 0 {
		return 1, 1
	}
	hi = min(hi, maxPlayers)
	if lo <= 0 || lo > hi {
		lo = 1
	}
	return lo, hi
}

type planUnit struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Model string `json:"model"`
	BV    int    `json:"bv"`
}

type planCrew struct {
	Unit     string `json:"unit"`
	UnitName string `json:"unitName"`
	Pilot    string `json:"pilot"`
	Gunnery  int    `json:"gunnery"`
	Piloting int    `json:"piloting"`
}

type planPlayer struct {
	Player int        `json:"player"`
	Units  []planUnit `json:"units"`
	Crew   []planCrew `json:"crew"`
	BV     int        `json:"bv"`
}

type planResult struct {
	Players  []planPlayer `json:"players"`
	TotalBV  int          `json:"totalBV"`
	Limit    int          `json:"limit"`
	Deployed bool         `json:"deployed"` // always false for now: nothing was sent to MekBay
	// Lobby says who will own each force of the multiplayer lobby operation (a player's own MekBay account, or the site's for the OpFor), never the accounts themselves.
	Lobby []lobbyForceView `json:"lobby"`
}

// rulesProblem checks a plan against the scenario's own rules, beyond the BV limit: the most mechs in all (unit_limit), and a named pilot on every mech
// (pilots_required). units[i] and crews[i] are player i+1's mech IDs and pilot assignments. It returns the first problem as a sentence, or "".
func (sc *Scenario) rulesProblem(units [][]string, crews [][]CrewAssignment) string {
	total := 0
	for _, u := range units {
		total += len(u)
	}
	if sc.UnitLimit > 0 && total > sc.UnitLimit {
		return fmt.Sprintf("This mission allows at most %d mechs in all; you chose %d.", sc.UnitLimit, total)
	}
	if sc.PilotsRequired {
		for i, u := range units {
			named := map[string]bool{}
			if i < len(crews) {
				for _, a := range crews[i] {
					if a.Name != "" {
						named[a.UnitID] = true
					}
				}
			}
			for _, id := range u {
				if !named[id] {
					return "Every mech on this mission needs a named pilot: assign one to each."
				}
			}
		}
	}
	return ""
}

// planProblem checks a whole plan against the hangar and returns the first problem as a sentence, or "". units[i] is player i+1's mech IDs.
func planProblem(h HangarForce, units [][]string, bvLimit int) string {
	var all []string
	for i, u := range units {
		if len(u) == 0 {
			return fmt.Sprintf("Player %d has no mechs. Each player needs at least one.", i+1)
		}
		all = append(all, u...)
	}
	// The same rules as a single-player deploy, over every player's mechs together: each exists, none is destroyed or unknown, none is taken twice, and the whole
	// force fits the scenario's BV limit.
	return deployProblem(h, all, bvLimit)
}

// deployPlan is POST /api/deploy/plan: check a deployment plan and describe it. Fields: campaign, scenario, players, and for each player i the repeated
// p<i>_units and the paired p<i>_assign_unit and p<i>_assign_pilot. It needs a started campaign but not modify mode, since it writes nothing.
func (s *Server) deployPlan(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	pt, run, c, sc, ok := s.mekbayRequest(w, r)
	if !ok {
		return
	}
	lo, hi := playerRange(c)
	n, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("players")))
	if err != nil || n < lo || n > hi {
		msg := fmt.Sprintf("%s can be played by %d to %d players.", c.Label, lo, hi)
		if lo == hi {
			msg = fmt.Sprintf("%s is played by %d player(s).", c.Label, lo)
		}
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	h, err := s.scenarioHangar(r.Context(), run, sc)
	if err != nil {
		log.Printf("read hangar for the plan: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	pilots, err := s.store.Pilots(r.Context(), pt.ID, c.Slug)
	if err != nil {
		http.Error(w, "could not load your pilots", http.StatusInternalServerError)
		return
	}
	units := make([][]string, n)
	for i := range units {
		units[i] = r.PostForm["p"+strconv.Itoa(i+1)+"_units"]
	}
	if msg := planProblem(h, units, sc.BVLimit); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	views := map[string]hangarUnitView{}
	for _, v := range hangarViews(h) {
		views[v.ID] = v
	}
	res := planResult{Players: []planPlayer{}, Limit: sc.BVLimit, Lobby: []lobbyForceView{}}
	pilotUsed := map[string]int{}
	crews := make([][]CrewAssignment, n)
	for i := 0; i < n; i++ {
		num := i + 1
		pre := "p" + strconv.Itoa(num) + "_"
		mine := map[string]bool{}
		pp := planPlayer{Player: num, Units: []planUnit{}, Crew: []planCrew{}}
		for _, id := range units[i] {
			v := views[id]
			mine[id] = true
			pp.Units = append(pp.Units, planUnit{ID: id, Name: v.Name, Model: v.Model, BV: v.BV})
			pp.BV += v.BV
		}
		crew, err := parseCrewPairs(r.PostForm[pre+"assign_unit"], r.PostForm[pre+"assign_pilot"], h, pilots)
		if err != nil {
			http.Error(w, fmt.Sprintf("Player %d: %s", num, err.Error()), http.StatusBadRequest)
			return
		}
		for _, a := range crew {
			if !mine[a.UnitID] {
				http.Error(w, fmt.Sprintf("Player %d assigned a pilot to a mech they aren't taking.", num), http.StatusBadRequest)
				return
			}
			if a.Name == "" {
				continue
			}
			if other, dup := pilotUsed[strings.ToLower(a.Name)]; dup {
				http.Error(w, fmt.Sprintf("%s is assigned to both Player %d and Player %d.", a.Name, other, num), http.StatusBadRequest)
				return
			}
			pilotUsed[strings.ToLower(a.Name)] = num
			pp.Crew = append(pp.Crew, planCrew{Unit: a.UnitID, UnitName: views[a.UnitID].Name, Pilot: a.Name, Gunnery: a.Gunnery, Piloting: a.Piloting})
		}
		crews[i] = crew
		res.Players = append(res.Players, pp)
		res.TotalBV += pp.BV
	}
	if msg := sc.rulesProblem(units, crews); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	// Build the lobby's plan (who owns each force) now, so a plan the real deploy couldn't carry out is refused here.
	lobby, err := buildLobbyPlan(currentSeed(r), units, crews)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	res.Lobby = lobby.View()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(res)
}
