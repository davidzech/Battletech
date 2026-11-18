package site

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestBuildLobbyPlan(t *testing.T) {
	const seed = "wolfpack2026"
	units := [][]string{{"ua", "ub"}, {"uc"}}
	crews := [][]CrewAssignment{{{UnitID: "ua", Name: "Alrek"}}, nil}
	plan, err := buildLobbyPlan(seed, units, crews)
	if err != nil {
		t.Fatal(err)
	}
	// Every force's owner is derived from the seed: each player's own account, the OpFor's and the hangar's.
	if len(plan.Players) != 2 || plan.Players[0].Owner != MekBayUID(seed, SlotPlayer(1)) || plan.Players[1].Owner != MekBayUID(seed, SlotPlayer(2)) {
		t.Errorf("players: %+v", plan.Players)
	}
	if plan.OpFor.Owner != MekBayUID(seed, SlotOpFor) || plan.Hangar != MekBayUID(seed, SlotHangar) {
		t.Errorf("the OpFor and the hangar have their own accounts: %+v", plan)
	}
	owners := map[string]bool{}
	for _, o := range []string{plan.Players[0].Owner, plan.Players[1].Owner, plan.OpFor.Owner, plan.Hangar} {
		if owners[o] {
			t.Errorf("every force needs its own owner, but %s repeats", o)
		}
		owners[o] = true
	}
	if !reflect.DeepEqual(plan.Players[0].Units, []string{"ua", "ub"}) || plan.Players[0].Crew[0].Name != "Alrek" || plan.Players[1].Player != 2 || plan.Players[1].Name != "Player 2" {
		t.Errorf("what each force holds: %+v", plan.Players)
	}
	// The accounts never reach JSON, in the plan or in its view.
	for _, v := range []any{plan, plan.View()} {
		b, _ := json.Marshal(v)
		for secret := range owners {
			if strings.Contains(string(b), secret) {
				t.Errorf("an account ID leaked into JSON: %s", b)
			}
		}
	}
	if got := plan.View(); len(got) != 3 || got[0] != (lobbyForceView{"Player 1", "player", 2}) || got[2] != (lobbyForceView{"OpFor", "site", 0}) {
		t.Errorf("view: %+v", got)
	}
	// Refusals: counts out of range and no seed.
	if _, err := buildLobbyPlan(seed, nil, nil); err == nil {
		t.Error("a lobby needs players")
	}
	if _, err := buildLobbyPlan(seed, make([][]string, maxPlayers+1), nil); err == nil {
		t.Error("a lobby has at most four players")
	}
	if _, err := buildLobbyPlan("", units, crews); err == nil {
		t.Error("a lobby needs a seed")
	}
}

// Starting a campaign asks how many players, within the campaign's range, and needs nothing else from them.
func TestStartNeedsAPlayerCount(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	id := playthroughID("wolfpack2026")
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	start := func(players string) *http.Response {
		v := startForm(goodHangar, "Alrek", "Katarina")
		if players == "" {
			v.Del("players")
		} else {
			v.Set("players", players)
		}
		return post(t, srv, "/api/campaign/start", v, cookie, srv.URL)
	}
	started := func() bool { _, ok, _ := store.Run(ctx, id, "refusalwar"); return ok }
	// Refusal War allows 1 to 2 players: a count is required and must be within range.
	for name, players := range map[string]string{"none sent": "", "zero": "0", "too many": "3", "not a number": "two"} {
		if r := start(players); r.StatusCode != http.StatusBadRequest || started() {
			t.Errorf("%s: %d", name, r.StatusCode)
		}
	}
	if r := start("2"); r.StatusCode != http.StatusNoContent {
		t.Fatalf("two players, with no MekBay user IDs anywhere: %d", r.StatusCode)
	}
	if run, _, _ := store.Run(ctx, id, "refusalwar"); run.Players != 2 {
		t.Errorf("the chosen count is saved as the run's default: %+v", run)
	}
}

func TestOverviewDefaultPlayers(t *testing.T) {
	srv, store, cookie := startedCompany(t) // started with one player
	ctx := context.Background()
	pt, _, _ := store.Playthrough(ctx, playthroughID("wolfpack2026"))
	set := func(players string) int {
		return post(t, srv, "/api/campaign/players", url.Values{"campaign": {"refusalwar"}, "players": {players}}, cookie, srv.URL).StatusCode
	}
	run := func() int { r, _, _ := store.Run(ctx, pt.ID, "refusalwar"); return r.Players }
	if got := post(t, srv, "/api/campaign/players", url.Values{"campaign": {"refusalwar"}, "players": {"2"}}, cookie, "https://evil.example").StatusCode; got != http.StatusForbidden {
		t.Errorf("cross-origin: %d", got)
	}
	if run() != 1 {
		t.Fatalf("started with one player: %d", run())
	}
	for _, bad := range []string{"", "0", "3", "x"} {
		if got := set(bad); got != http.StatusBadRequest || run() != 1 {
			t.Errorf("players=%q: %d", bad, got)
		}
	}
	if got := set("2"); got != http.StatusNoContent || run() != 2 {
		t.Errorf("two players, with no MekBay user IDs anywhere: %d, saved %d", got, run())
	}
	page := body(t, srv, "/refusalwar/manage/", cookie)
	for _, want := range []string{`id="overview"`, `id="force-name"`, `data-players="2"`, "/static/overview.js"} {
		if !strings.Contains(page, want) {
			t.Errorf("the Manage page is missing %q", want)
		}
	}
	if strings.Contains(page, ">Warchest<") || strings.Contains(page, "data-player-ids") {
		t.Error("the section is Overview now, and says nothing of saved IDs")
	}
}
