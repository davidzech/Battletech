package site

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestPlayerRange(t *testing.T) {
	for _, c := range []struct {
		lo, hi, wantLo, wantHi int
	}{{1, 2, 1, 2}, {0, 0, 1, 1}, {2, 4, 2, 4}, {3, 9, 3, 4}, {5, 2, 1, 2}, {0, 3, 1, 3}} {
		if lo, hi := playerRange(&Campaign{MinPlayers: c.lo, MaxPlayers: c.hi}); lo != c.wantLo || hi != c.wantHi {
			t.Errorf("min %d max %d: %d-%d, want %d-%d", c.lo, c.hi, lo, hi, c.wantLo, c.wantHi)
		}
	}
}

func TestPlanProblem(t *testing.T) {
	h := hangarOf("BMMadCat_H", "BMKoshi_E", "BMGrizzly", "BMUller_J")
	h.Units[3].Destroyed = true
	cases := map[string]struct {
		units [][]string
		limit int
		bad   string // part of the expected problem, "" for none
	}{
		"one player":            {[][]string{{"ua", "ub"}}, 10000, ""},
		"two players split":     {[][]string{{"ua"}, {"ub", "uc"}}, 10000, ""},
		"a player with nothing": {[][]string{{"ua"}, {}}, 10000, "Player 2 has no mechs"},
		"a mech taken twice":    {[][]string{{"ua"}, {"ua"}}, 10000, "twice"},
		"a destroyed mech":      {[][]string{{"ua"}, {"ud"}}, 10000, "destroyed"},
		"not in the hangar":     {[][]string{{"ua"}, {"zz"}}, 10000, "no longer in your hangar"},
		"over the limit":        {[][]string{{"ua"}, {"ub", "uc"}}, 3000, "over"},
	}
	for name, c := range cases {
		got := planProblem(h, c.units, c.limit)
		if (c.bad == "") != (got == "") || (c.bad != "" && !strings.Contains(strings.ToLower(got), strings.ToLower(c.bad))) {
			t.Errorf("%s: %q, want %q", name, got, c.bad)
		}
	}
}

func TestDeployPlanEndpoint(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()} // ua Koshi, ub Uller, uc Dasher, ud Grizzly
	srv, store := newMekBayTestServer(t, fake)
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	post(t, srv, "/api/company", url.Values{"uid_p1": {uidP1}, "company": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	id := playthroughID("wolfpack2026")
	pilots, _ := store.Pilots(t.Context(), id, "refusalwar")
	var alrek NamedPilot
	for _, p := range pilots {
		if p.Name == "Alrek" {
			alrek = p
		}
	}
	plan := func(kv ...string) *http.Response {
		v := url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_001"}}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Add(kv[i], kv[i+1])
		}
		return post(t, srv, "/api/deploy/plan", v, cookie, srv.URL)
	}
	read := func(res *http.Response) planResult {
		t.Helper()
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("plan: %d %s", res.StatusCode, b)
		}
		var out planResult
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("%v: %s", err, b)
		}
		return out
	}
	if got := post(t, srv, "/api/deploy/plan", url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_001"}, "players": {"1"}, "p1_units": {"ua"}}, cookie, "https://evil.example").StatusCode; got != http.StatusForbidden {
		t.Errorf("cross-origin: %d", got)
	}

	// One player.
	one := read(plan("players", "1", "p1_units", "ua", "p1_units", "ub", "p1_assign_unit", "ua", "p1_assign_pilot", "alrek"))
	if len(one.Players) != 1 || len(one.Players[0].Units) != 2 || one.Deployed || one.Limit != 10000 {
		t.Fatalf("one player: %+v", one)
	}
	if c := one.Players[0].Crew; len(c) != 1 || c[0].Pilot != "Alrek" || c[0].Gunnery != alrek.Gunnery() || c[0].Piloting != alrek.Piloting() || c[0].UnitName == "" {
		t.Errorf("crew: %+v", one.Players[0].Crew)
	}
	// Two players need Player 2's MekBay user ID saved too, since each takes their own force into the lobby.
	if got := plan("players", "2", "p1_units", "ua", "p2_units", "ub").StatusCode; got != http.StatusBadRequest {
		t.Errorf("two players without Player 2's ID: %d", got)
	}
	post(t, srv, "/api/company", url.Values{"uid_p1": {uidP1}, "uid_p2": {uidP2}, "company": {"Wolf Pack"}}, cookie, srv.URL)
	// Two players split the mechs and the pilots; the total BV is the two players' together.
	two := read(plan("players", "2", "p1_units", "ua", "p1_units", "ub", "p1_assign_unit", "ua", "p1_assign_pilot", "Alrek",
		"p2_units", "uc", "p2_assign_unit", "uc", "p2_assign_pilot", "Katarina"))
	if len(two.Players) != 2 || two.Players[0].Player != 1 || two.Players[1].Player != 2 || two.TotalBV != two.Players[0].BV+two.Players[1].BV || two.TotalBV == 0 {
		t.Fatalf("two players: %+v", two)
	}
	if two.Players[1].Crew[0].Pilot != "Katarina" || two.Players[1].Units[0].ID != "uc" {
		t.Errorf("player 2: %+v", two.Players[1])
	}
	// The plan says who will own each force in the lobby, never the accounts.
	if len(two.Lobby) != 3 || two.Lobby[0] != (lobbyForceView{"Player 1", "player", 2}) || two.Lobby[1] != (lobbyForceView{"Player 2", "player", 1}) || two.Lobby[2] != (lobbyForceView{"OpFor", "site", 0}) {
		t.Errorf("lobby: %+v", two.Lobby)
	}
	// Nothing is saved or sent to MekBay.
	if len(fake.reqs) != 0 || len(fake.services) != 0 || len(fake.purchases) != 0 {
		t.Errorf("a plan must not touch MekBay: %+v", fake.reqs)
	}
	bad := map[string][]string{
		"no players":           {"p1_units", "ua"},
		"too many players":     {"players", "3", "p1_units", "ua", "p2_units", "ub", "p3_units", "uc"},
		"zero players":         {"players", "0", "p1_units", "ua"},
		"a player with nobody": {"players", "2", "p1_units", "ua"},
		"a mech twice":         {"players", "2", "p1_units", "ua", "p2_units", "ua"},
		"a pilot twice":        {"players", "2", "p1_units", "ua", "p1_assign_unit", "ua", "p1_assign_pilot", "Alrek", "p2_units", "ub", "p2_assign_unit", "ub", "p2_assign_pilot", "alrek"},
		"unknown pilot":        {"players", "1", "p1_units", "ua", "p1_assign_unit", "ua", "p1_assign_pilot", "Nobody"},
		"a mech not taken":     {"players", "2", "p1_units", "ua", "p2_units", "ub", "p2_assign_unit", "ua", "p2_assign_pilot", "Alrek"},
		"unpaired":             {"players", "1", "p1_units", "ua", "p1_assign_unit", "ua"},
	}
	for name, kv := range bad {
		if got := plan(kv...).StatusCode; got != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, got)
		}
	}
}
