package site

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/pilot"
	"github.com/davidzech/Battletech/internal/storage"
)

// The deployment plan: which mechs and pilots each player takes on a scenario. The Deploy wizard steps through the players one at a time, each choosing from what
// the earlier ones left, and ends with a review of the whole plan. This endpoint checks that plan on the server and describes it back. It does not touch MekBay yet:
// building each player's force in their own MekBay account, and the OpFor in the OpFor's, comes next, and the old single-player deploy (POST /api/mekbay/deploy)
// is still in the code but is not used by the wizard.

// playerRange is how many players a campaign can be deployed with (Intro.md min_players and max_players); a campaign that doesn't say is a one-player campaign.
func playerRange(c *content.Campaign) (lo, hi int) {
	lo, hi = c.MinPlayers, c.MaxPlayers
	if hi <= 0 {
		return 1, 1
	}
	hi = min(hi, mekbay.MaxPlayers)
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
	// Commander is the named pilot who commands the force (marked on their mech in MekBay); "" when the scenario has none or no named pilot is deployed.
	Commander string `json:"commander,omitempty"`
}

// planProblem checks a whole plan against the hangar and returns the first problem as a sentence, or "". units[i] is player i+1's mech IDs.
func planProblem(h mekbay.HangarForce, units [][]string, bvLimit int) string {
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

// builtPlan is a deployment plan that has passed every check.
type builtPlan struct {
	pt    storage.Playthrough
	run   storage.CampaignRun
	c     *content.Campaign
	sc    *content.Scenario
	h     mekbay.HangarForce // the force the players take their mechs from (the hangar, or the scenario's provided mechs)
	views map[string]hangarUnitView
	units [][]string // each player's mechs
	crews [][]mekbay.CrewAssignment
	res   planResult
	// commander is the deployed named pilot who commands the force, and where: their player (0-based) and mech. nil when there is none.
	commander *deployedPilot
}

// deployedPilot is a named pilot in one of the plan's mechs.
type deployedPilot struct {
	player int // 0-based
	unit   string
	name   string
}

// checkPlan reads and checks a deployment plan from a request, and answers the request itself when it is not good. Fields: campaign, scenario, players, and for each player i the repeated
// p<i>_units and the paired p<i>_assign_unit and p<i>_assign_pilot. It needs a started campaign and writes nothing.
func (s *Server) checkPlan(w http.ResponseWriter, r *http.Request) (*builtPlan, bool) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return nil, false
	}
	pt, run, c, sc, ok := s.mekbayRequest(w, r)
	if !ok {
		return nil, false
	}
	lo, hi := playerRange(c)
	n, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("players")))
	if err != nil || n < lo || n > hi {
		msg := fmt.Sprintf("%s can be played by %d to %d players.", c.Label, lo, hi)
		if lo == hi {
			msg = fmt.Sprintf("%s is played by %d player(s).", c.Label, lo)
		}
		http.Error(w, msg, http.StatusBadRequest)
		return nil, false
	}
	h, err := s.scenarioHangar(r.Context(), run, sc)
	if err != nil {
		log.Printf("read hangar for the plan: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return nil, false
	}
	pilots, err := s.store.Pilots(r.Context(), pt.ID, c.Slug)
	if err != nil {
		http.Error(w, "could not load your pilots", http.StatusInternalServerError)
		return nil, false
	}
	units := make([][]string, n)
	for i := range units {
		units[i] = r.PostForm["p"+strconv.Itoa(i+1)+"_units"]
	}
	if msg := planProblem(h, units, sc.BVLimit); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return nil, false
	}
	views := map[string]hangarUnitView{}
	for _, v := range hangarViews(h) {
		views[v.ID] = v
	}
	res := planResult{Players: []planPlayer{}, Limit: sc.BVLimit}
	pilotUsed := map[string]int{}
	crews := make([][]mekbay.CrewAssignment, n)
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
			return nil, false
		}
		for _, a := range crew {
			if !mine[a.UnitID] {
				http.Error(w, fmt.Sprintf("Player %d assigned a pilot to a mech they aren't taking.", num), http.StatusBadRequest)
				return nil, false
			}
			if a.Name == "" {
				continue
			}
			if other, dup := pilotUsed[strings.ToLower(a.Name)]; dup {
				http.Error(w, fmt.Sprintf("%s is assigned to both Player %d and Player %d.", a.Name, other, num), http.StatusBadRequest)
				return nil, false
			}
			pilotUsed[strings.ToLower(a.Name)] = num
			pp.Crew = append(pp.Crew, planCrew{Unit: a.UnitID, UnitName: views[a.UnitID].Name, Pilot: a.Name, Gunnery: a.Gunnery, Piloting: a.Piloting})
		}
		crews[i] = crew
		res.Players = append(res.Players, pp)
		res.TotalBV += pp.BV
	}
	if msg := sc.RulesProblem(units, crews); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return nil, false
	}
	commander, msg := planCommander(sc, h, pilots, units, crews, r.PostFormValue("commander"))
	if msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return nil, false
	}
	if commander != nil {
		res.Commander = commander.name
	}
	return &builtPlan{pt: pt, run: run, c: c, sc: sc, h: h, views: views, units: units, crews: crews, res: res, commander: commander}, true
}

// deployedPilots lists the living named pilots the plan puts in a mech, in player and mech order: a mech's pilot is the one assigned to it, or the one the force
// already has in it when the plan doesn't change it.
func deployedPilots(h mekbay.HangarForce, pilots []pilot.NamedPilot, units [][]string, crews [][]mekbay.CrewAssignment) []deployedPilot {
	living := map[string]string{}
	for _, p := range pilots {
		if !p.Fallen {
			living[strings.ToLower(p.Name)] = p.Name
		}
	}
	inForce := map[string]string{}
	for _, u := range h.Units {
		inForce[u.ID] = u.Pilot
	}
	var out []deployedPilot
	for i, ids := range units {
		assigned := map[string]string{}
		for _, a := range crews[i] {
			assigned[a.UnitID] = a.Name
		}
		for _, id := range ids {
			name, ok := assigned[id]
			if !ok {
				name = inForce[id]
			}
			if canonical, ok := living[strings.ToLower(name)]; ok {
				out = append(out, deployedPilot{player: i, unit: id, name: canonical})
			}
		}
	}
	return out
}

// planCommander settles who commands the force: none when the scenario doesn't choose one or no named pilot is deployed, the only one when there is one, and
// otherwise the deployed named pilot the request names (`commander`, a pilot's name, any case). The second result is the problem, as a sentence.
func planCommander(sc *content.Scenario, h mekbay.HangarForce, pilots []pilot.NamedPilot, units [][]string, crews [][]mekbay.CrewAssignment, want string) (*deployedPilot, string) {
	if !sc.ChoosesCommander {
		return nil, ""
	}
	deployed := deployedPilots(h, pilots, units, crews)
	switch {
	case len(deployed) == 0:
		return nil, ""
	case len(deployed) == 1:
		return &deployed[0], ""
	}
	want = strings.TrimSpace(want)
	if want == "" {
		return nil, "Choose which named pilot commands the force."
	}
	for i := range deployed {
		if strings.EqualFold(deployed[i].name, want) {
			return &deployed[i], ""
		}
	}
	return nil, want + " isn't in one of the mechs being deployed, so they can't command the force."
}

// deployPlan is POST /api/deploy/plan: check a deployment plan and describe it, without sending anything to MekBay.
func (s *Server) deployPlan(w http.ResponseWriter, r *http.Request) {
	bp, ok := s.checkPlan(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(bp.res)
}
