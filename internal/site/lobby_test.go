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
	uids := [maxPlayers]string{uidP1, uidP2, "", ""}
	units := [][]string{{"ua", "ub"}, {"uc"}}
	crews := [][]CrewAssignment{{{UnitID: "ua", Name: "Alrek"}}, nil}
	plan, err := buildLobbyPlan(seed, uids, units, crews)
	if err != nil {
		t.Fatal(err)
	}
	// Each player's force is owned by their own MekBay account; the OpFor and the hangar by the site's synthesized ones.
	if len(plan.Players) != 2 || plan.Players[0].Owner != uidP1 || plan.Players[1].Owner != uidP2 {
		t.Errorf("players: %+v", plan.Players)
	}
	if plan.OpFor.Owner != MekBayUID(seed, SlotOpFor) || plan.Hangar != MekBayUID(seed, SlotHangar) || plan.OpFor.Owner == plan.Hangar {
		t.Errorf("the OpFor and the hangar belong to the site's own accounts: %+v", plan)
	}
	if !reflect.DeepEqual(plan.Players[0].Units, []string{"ua", "ub"}) || plan.Players[0].Crew[0].Name != "Alrek" || plan.Players[1].Player != 2 || plan.Players[1].Name != "Player 2" {
		t.Errorf("what each force holds: %+v", plan.Players)
	}
	// The IDs never reach JSON, in the plan or in its view.
	for _, v := range []any{plan, plan.View()} {
		b, _ := json.Marshal(v)
		for _, secret := range []string{uidP1, uidP2, plan.Hangar, plan.OpFor.Owner} {
			if strings.Contains(string(b), secret) {
				t.Errorf("an account ID leaked into JSON: %s", b)
			}
		}
	}
	if got := plan.View(); len(got) != 3 || got[0] != (lobbyForceView{"Player 1", "player", 2}) || got[2] != (lobbyForceView{"OpFor", "site", 0}) {
		t.Errorf("view: %+v", got)
	}
	// Refusals: a player without an ID, a duplicate ID, an ID that is the site's own account, and counts out of range.
	if _, err := buildLobbyPlan(seed, [maxPlayers]string{uidP1}, units, crews); err == nil || !strings.Contains(err.Error(), "Player 2 needs a MekBay user ID") {
		t.Errorf("a missing ID: %v", err)
	}
	if _, err := buildLobbyPlan(seed, [maxPlayers]string{uidP1, uidP1}, units, crews); err == nil {
		t.Error("two players can't share an owner")
	}
	if _, err := buildLobbyPlan(seed, [maxPlayers]string{MekBayUID(seed, SlotOpFor)}, units[:1], nil); err == nil {
		t.Error("a player must never own the OpFor")
	}
	if _, err := buildLobbyPlan(seed, uids, nil, nil); err == nil {
		t.Error("a lobby needs players")
	}
	if _, err := buildLobbyPlan("", uids, units, crews); err == nil {
		t.Error("a lobby needs a seed")
	}
}

func TestStartNeedsPlayersAndTheirIDs(t *testing.T) {
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
	// Refusal War allows 1 to 2 players: a count is required, within range, and needs those players' IDs.
	for name, players := range map[string]string{"none sent": "", "zero": "0", "too many": "3", "not a number": "two"} {
		if r := start(players); r.StatusCode != http.StatusBadRequest || started() {
			t.Errorf("%s: %d", name, r.StatusCode)
		}
	}
	if r := start("1"); r.StatusCode != http.StatusBadRequest || started() {
		t.Errorf("without any player ID: %d", r.StatusCode)
	}
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}, "uid_p1": {uidP1}}, cookie, srv.URL)
	if r := start("2"); r.StatusCode != http.StatusBadRequest || started() {
		t.Errorf("two players with only Player 1's ID: %d", r.StatusCode)
	}
	if r := start("1"); r.StatusCode != http.StatusNoContent {
		t.Fatalf("one player with Player 1's ID: %d", r.StatusCode)
	}
	if run, _, _ := store.Run(ctx, id, "refusalwar"); run.Players != 1 {
		t.Errorf("the chosen count is saved as the run's default: %+v", run)
	}
}

func TestOverviewDefaultPlayers(t *testing.T) {
	srv, store, cookie := startedCompany(t) // started with one player and Player 1's ID
	ctx := context.Background()
	pt, _, _ := store.Playthrough(ctx, playthroughID("wolfpack2026"))
	set := func(players string) int {
		return post(t, srv, "/api/campaign/players", url.Values{"campaign": {"refusalwar"}, "players": {players}}, cookie, srv.URL).StatusCode
	}
	run := func() int { r, _, _ := store.Run(ctx, pt.ID, "refusalwar"); return r.Players }
	if got := post(t, srv, "/api/campaign/players", url.Values{"campaign": {"refusalwar"}, "players": {"1"}}, cookie, "https://evil.example").StatusCode; got != http.StatusForbidden {
		t.Errorf("cross-origin: %d", got)
	}
	if run() != 1 {
		t.Fatalf("started with one player: %d", run())
	}
	if got := set("2"); got != http.StatusBadRequest || run() != 1 {
		t.Errorf("two players needs Player 2's ID: %d, saved %d", got, run())
	}
	for _, bad := range []string{"", "0", "3", "x"} {
		if got := set(bad); got != http.StatusBadRequest || run() != 1 {
			t.Errorf("players=%q: %d", bad, got)
		}
	}
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}, "uid_p1": {uidP1}, "uid_p2": {uidP2}}, cookie, srv.URL)
	if got := set("2"); got != http.StatusNoContent || run() != 2 {
		t.Errorf("two players with both IDs: %d, saved %d", got, run())
	}
	page := body(t, srv, "/refusalwar/manage/", cookie)
	for _, want := range []string{`id="overview"`, `<h2 class="section-title">Overview</h2>`, `data-players="2"`, `data-player-ids="[true,true,false,false]"`, "/static/overview.js"} {
		if !strings.Contains(page, want) {
			t.Errorf("the Manage page is missing %q", want)
		}
	}
	if strings.Contains(page, ">Warchest<") || strings.Contains(page, uidP1) || strings.Contains(page, uidP2) {
		t.Error("the section is Overview now, and shows no IDs")
	}
}
