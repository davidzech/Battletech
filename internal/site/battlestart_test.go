package site

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func readBattle(t *testing.T, res *http.Response) battleResponse {
	t.Helper()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("start battle: %d %s", res.StatusCode, b)
	}
	var out battleResponse
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	return out
}

var battleURL = regexp.MustCompile(`^http://mekbay-([a-z2-7]{32})\.([^/]+)/\?operation=([0-9a-f-]{36})$`)

func TestStartBattleEndpoint(t *testing.T) {
	const seed = "wolfpack2026"
	fake := &fakeMekBay{hangar: startingHangar()} // ua Koshi, ub Uller, uc Dasher, ud Grizzly
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {seed}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	id := playthroughID(seed)
	run, _, _ := store.Run(ctx, id, "refusalwar")
	pilots, _ := store.Pilots(ctx, id, "refusalwar")
	var alrek NamedPilot
	for _, p := range pilots {
		if p.Name == "Alrek" {
			alrek = p
		}
	}
	start := func(origin string, kv ...string) *http.Response {
		v := url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_001"}}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Add(kv[i], kv[i+1])
		}
		return post(t, srv, "/api/battle/start", v, cookie, origin)
	}
	two := []string{"players", "2", "p1_units", "ua", "p1_assign_unit", "ua", "p1_assign_pilot", "Alrek", "p2_units", "ub"}

	if got := start("https://evil.example", two...).StatusCode; got != http.StatusForbidden || len(fake.battles) != 0 {
		t.Errorf("cross-origin: %d", got)
	}
	// A plan that doesn't pass the checks never reaches MekBay and is never recorded.
	for name, kv := range map[string][]string{
		"no mechs":     {"players", "2", "p1_units", "ua"},
		"a mech twice": {"players", "2", "p1_units", "ua", "p2_units", "ua"},
		"no players":   {"p1_units", "ua"},
	} {
		if got := start(srv.URL, kv...).StatusCode; got != http.StatusBadRequest {
			t.Errorf("%s: %d", name, got)
		}
	}
	if len(fake.battles) != 0 {
		t.Fatalf("a bad plan reached MekBay: %d", len(fake.battles))
	}
	if _, ok, _ := store.MekBayOperation(ctx, id, "refusalwar", "scenario_001"); ok {
		t.Fatal("a bad plan started the scenario")
	}

	// A good plan: MekBay is asked to build every force under the seed's one account, and the operation that sets them against each other.
	first := readBattle(t, start(srv.URL, two...))
	if len(fake.battles) != 1 {
		t.Fatalf("one start: %d", len(fake.battles))
	}
	req := fake.battles[0]
	if req.Account != MekBayUID(seed, SlotHangar) || req.Provided || req.HangarInstance != run.HangarInstance || len(req.Players) != 2 || req.OperationName != "Refusal War - Sortie 001" {
		t.Fatalf("the request: %+v", req)
	}
	for i, p := range req.Players {
		if p.Number != i+1 || !strings.HasSuffix(p.Name, "Player "+string(rune('1'+i))) {
			t.Errorf("player %d: %+v", i+1, p)
		}
	}
	if req.OpFor.Reuse != "" || len(req.OpFor.Draft.Groups) == 0 || req.OperationID != "" {
		t.Errorf("the OpFor and the operation are new the first time: %+v", req)
	}
	if len(req.Players[0].Units) != 1 || req.Players[0].Units[0] != "ua" || len(req.Players[1].Units) != 1 || req.Players[1].Units[0] != "ub" {
		t.Errorf("each player's mechs: %+v", req.Players)
	}
	if c := req.Players[0].Crew; len(c) != 1 || c[0].UnitID != "ua" || c[0].Name != "Alrek" || c[0].Gunnery != alrek.Gunnery() || c[0].Piloting != alrek.Piloting() {
		t.Errorf("Player 1's pilot: %+v", c)
	}

	// The answer is one link for the whole table: a mekbay-<id> host of its own opening the operation, never showing the account.
	m := battleURL.FindStringSubmatch(first.URL)
	if m == nil || m[3] != first.Operation {
		t.Fatalf("the link opens the operation on a mekbay-<id> host: %q (%s)", first.URL, first.Operation)
	}
	if strings.Contains(first.URL, MekBayUID(seed, SlotHangar)) {
		t.Errorf("the link never shows the account: %q", first.URL)
	}
	// The scenario is recorded as started, with its operation and OpFor.
	op, ok, err := store.MekBayOperation(ctx, id, "refusalwar", "scenario_001")
	if err != nil || !ok || op.OperationID != first.Operation || op.OpForInstance == "" {
		t.Fatalf("the scenario is recorded as started: %+v %v %v", op, ok, err)
	}
	// While the battle is in progress the page offers View operation instead of Deploy force, and no separate Open in MekBay button.
	if page := body(t, srv, "/refusalwar/scenario_001/", cookie); !strings.Contains(page, `id="deploy-open">View operation</button>`) || !strings.Contains(page, `data-in-progress="1"`) || strings.Contains(page, "Open in MekBay") || strings.Contains(page, `>Deploy force</button>`) {
		t.Errorf("the scenario page shows the battle in progress")
	} else if !strings.Contains(page, `data-operation-url="`+first.URL+`"`) {
		t.Errorf("the page already carries the table's link, so the operation view can show it before anything is read from MekBay")
	}
	// The mission log says IN PROGRESS, with a button that opens the operation, instead of Not completed.
	if page := body(t, srv, "/refusalwar/scenario_001/", cookie); !strings.Contains(page, ` in-progress" id="mission-log"`) || !strings.Contains(page, `id="mission-log-open"`) || !strings.Contains(page, "<span>In progress</span>") || strings.Contains(page, "Not completed") {
		t.Errorf("the mission log shows the mission in progress: %.600s", page)
	}
	if page := body(t, srv, "/refusalwar/scenario_001/", cookie); strings.Contains(page, `id="manage-link"`) {
		t.Error("Manage force is hidden while the operation is in progress")
	}
	if hb := body(t, srv, "/api/mekbay/hangar?campaign=refusalwar&scenario=scenario_001", cookie); !strings.Contains(hb, `"hasOperation":true`) {
		t.Errorf("a started scenario has an OpFor to reset: %.200s", hb)
	}

	// Starting again updates the same operation (so the link stays the same) and keeps the OpFor, unless it is reset.
	again := readBattle(t, start(srv.URL, two...))
	if fake.battles[1].OperationID != first.Operation || fake.battles[1].OpFor.Reuse != op.OpForInstance {
		t.Errorf("a repeated start keeps the operation and the OpFor: %+v", fake.battles[1])
	}
	if again.URL != first.URL {
		t.Errorf("so the link stays the same: %q vs %q", again.URL, first.URL)
	}
	readBattle(t, start(srv.URL, append([]string{"reset_opfor", "1"}, two...)...))
	if fake.battles[2].OpFor.Reuse != "" || fake.battles[2].OperationID != first.Operation {
		t.Errorf("reset_opfor rebuilds the OpFor but keeps the operation: %+v", fake.battles[2])
	}

	// MekBay failing leaves the record alone and says why.
	fake.mu.Lock()
	fake.battleErr = errors.New("MekBay says that account doesn't own this force")
	fake.mu.Unlock()
	before, _, _ := store.MekBayOperation(ctx, id, "refusalwar", "scenario_001")
	res := start(srv.URL, two...)
	if b, _ := io.ReadAll(res.Body); res.StatusCode != http.StatusBadGateway || !strings.Contains(string(b), "doesn't own") {
		t.Errorf("a MekBay failure is a 502 with the reason: %d %s", res.StatusCode, b)
	}
	if after, _, _ := store.MekBayOperation(ctx, id, "refusalwar", "scenario_001"); after != before {
		t.Errorf("a failed start doesn't change the record: %+v vs %+v", after, before)
	}

	// Deleting the campaign takes the record with it.
	if r := post(t, srv, "/api/campaign/delete", url.Values{"campaign": {"refusalwar"}}, cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", r.StatusCode)
	}
	if _, ok, _ := store.MekBayOperation(ctx, id, "refusalwar", "scenario_001"); ok {
		t.Error("deleting the campaign removes its operations")
	}
}

// A scenario that provides its own mechs has none in MekBay to copy: each player's force is built from the units they took.
func TestStartBattleWithProvidedMechs(t *testing.T) {
	const seed = "wolfpack2026"
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {seed}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	pilots, _ := store.Pilots(ctx, playthroughID(seed), "refusalwar")
	levels := map[string]NamedPilot{}
	for _, p := range pilots {
		levels[p.Name] = p
	}
	units := []string{providedPrefix + "bmblackhawk_prime", providedPrefix + "bmryoken_prime"}
	res := post(t, srv, "/api/battle/start", url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_000"}, "players": {"1"},
		"p1_units": units, "p1_assign_unit": units, "p1_assign_pilot": {"Alrek", "Katarina"}}, cookie, srv.URL)
	readBattle(t, res)
	if len(fake.battles) != 1 || !fake.battles[0].Provided {
		t.Fatalf("a provided battle: %+v", fake.battles)
	}
	d := fake.battles[0].Players[0].Draft
	if len(d.Groups) != 1 || len(d.Groups[0].Units) != 2 || d.Name == "" {
		t.Fatalf("the player's force is built from what they took: %+v", d)
	}
	for i, u := range d.Groups[0].Units {
		name := []string{"Alrek", "Katarina"}[i]
		if u.Unit != []string{"BMBlackHawk_Prime", "BMRyoken_Prime"}[i] || u.Pilot != name || u.Gunnery != levels[name].Gunnery() || u.Piloting != levels[name].Piloting() || u.BV == 0 {
			t.Errorf("unit %d: %+v", i, u)
		}
	}
}

// A battle in progress is read back from MekBay: every force with each mech's live condition, the link, and a way to give the battle up.
func TestBattleStatusAndAbort(t *testing.T) {
	const seed = "wolfpack2026"
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {seed}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	id := playthroughID(seed)
	q := "campaign=refusalwar&scenario=scenario_001"
	status := func() *http.Response { return get(t, srv, "/api/battle/status?"+q, cookie) }
	abort := func(origin string) *http.Response {
		return post(t, srv, "/api/battle/abort", url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_001"}}, cookie, origin)
	}

	// With no battle there is nothing to read or abort.
	if got := status().StatusCode; got != http.StatusNotFound {
		t.Errorf("status with no battle: %d", got)
	}
	if got := abort(srv.URL).StatusCode; got != http.StatusNotFound {
		t.Errorf("abort with no battle: %d", got)
	}
	if page := body(t, srv, "/refusalwar/scenario_001/", cookie); !strings.Contains(page, `id="deploy-open">Deploy force</button>`) || strings.Contains(page, "View operation") || !strings.Contains(page, `data-operation-url=""`) {
		t.Error("with no battle the page offers Deploy force")
	}

	start := readBattle(t, post(t, srv, "/api/battle/start", url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_001"}, "players": {"2"}, "p1_units": {"ua"}, "p2_units": {"ub"}}, cookie, srv.URL))

	// The status lists every force with its mechs described as the Hangar rows describe them.
	hurt := hangarOf("BMKoshi_E", "BMUller_J")
	hurt.Name = "Wolf Pack - Player 1"
	hurt.Units[0].Pilot, hurt.Units[0].Gunnery, hurt.Units[0].Piloting = "Alrek", 3, 4
	hurt.Units[0].Damaged, hurt.Units[0].AmmoUsed, hurt.Units[0].AmmoBins = true, 2, 1
	hurt.Units[0].Locations = map[string]LocationLoss{"CT": {Armor: 6}}
	second := hangarOf("BMGrizzly")
	second.Name = "Wolf Pack - Player 2"
	fake.mu.Lock()
	fake.status = BattleStatus{Forces: []BattleForce{{Alignment: "friendly", Force: hurt}, {Alignment: "friendly", Force: second}}}
	fake.mu.Unlock()
	res := status()
	if res.StatusCode != http.StatusOK || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("status: %d %v", res.StatusCode, res.Header)
	}
	var got battleStatusResponse
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.URL != start.URL || got.Operation != start.Operation || got.UpdatedAt == 0 || len(got.Forces) != 2 {
		t.Fatalf("the status: %+v", got)
	}
	if f := got.Forces[0]; f.Name != "Wolf Pack - Player 1" || f.Alignment != "friendly" || len(f.Units) != 2 || f.Units[0].Pilot != "Alrek" || f.Units[0].Gunnery != 3 || f.Units[0].Condition == "" || f.Units[0].Condition == "ok" || !f.Units[0].AmmoExpended || f.Units[0].Name == "" {
		t.Errorf("a player's force with its mechs' live condition: %+v", f)
	}
	if f := got.Forces[1]; f.Alignment != "friendly" || f.Name != "Wolf Pack - Player 2" || len(f.Units) != 1 || f.Units[0].Damaged {
		t.Errorf("Player 2's force: %+v", f)
	}
	// The status read is kept, so the next visit to the page carries it (and paints at once): never the link, which is built from the request.
	page := body(t, srv, "/refusalwar/scenario_001/", cookie)
	if !strings.Contains(page, `data-cached-status="`) || !strings.Contains(page, "Wolf Pack - Player 1") || !strings.Contains(page, "&#34;updatedAt&#34;:") {
		t.Errorf("the page carries the last status read: %.200s", page)
	}
	if forces, at, err := store.BattleStatusCache(ctx, id, "refusalwar", "scenario_001"); err != nil || at == 0 || strings.Contains(forces, "mekbay-") {
		t.Errorf("the cache holds the forces and no link: %q %d %v", forces, at, err)
	}
	if len(fake.statusOps) != 1 || fake.statusOps[0] != start.Operation {
		t.Errorf("MekBay was asked about the scenario's operation: %v", fake.statusOps)
	}
	// MekBay being unreachable is a 502 with the reason, and the battle stays recorded.
	fake.mu.Lock()
	fake.statusErr = errors.New("MekBay has no such operation any more")
	fake.mu.Unlock()
	if r := status(); r.StatusCode != http.StatusBadGateway {
		t.Errorf("an unreadable battle: %d", r.StatusCode)
	}
	if _, ok, _ := store.MekBayOperation(ctx, id, "refusalwar", "scenario_001"); !ok {
		t.Error("a failed read doesn't end the battle")
	}

	// Aborting gives the battle up: same-origin only, once, and the scenario is back to Deploy force.
	if got := abort("https://evil.example").StatusCode; got != http.StatusForbidden {
		t.Errorf("cross-origin abort: %d", got)
	}
	if got := abort(srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("abort: %d", got)
	}
	if _, ok, _ := store.MekBayOperation(ctx, id, "refusalwar", "scenario_001"); ok {
		t.Error("the battle is gone after an abort")
	}
	if got := abort(srv.URL).StatusCode; got != http.StatusNotFound {
		t.Errorf("aborting twice: %d", got)
	}
	if got := status().StatusCode; got != http.StatusNotFound {
		t.Errorf("status after an abort: %d", got)
	}
	if page := body(t, srv, "/refusalwar/scenario_001/", cookie); !strings.Contains(page, `id="deploy-open">Deploy force</button>`) {
		t.Error("after an abort the page offers Deploy force again")
	}
	// And the next start builds a new OpFor and operation, since nothing is kept.
	readBattle(t, post(t, srv, "/api/battle/start", url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_001"}, "players": {"1"}, "p1_units": {"ua"}}, cookie, srv.URL))
	if last := fake.battles[len(fake.battles)-1]; last.OperationID != "" || last.OpFor.Reuse != "" {
		t.Errorf("a start after an abort is a fresh one: %+v", last)
	}
}

// A battle ends when its mission is completed, however that is recorded.
func TestCompletingTheMissionEndsTheBattle(t *testing.T) {
	_, store, _ := startedCompany(t)
	ctx := context.Background()
	id := playthroughID("wolfpack2026")
	op := MekBayOperation{OpForInstance: "of", OperationID: "op"}
	for name, complete := range map[string]func(scenario string) error{
		"marked completed": func(sc string) error { return store.SetScenarioCompleted(ctx, id, "refusalwar", sc, true) },
		"completed with an outcome": func(sc string) error {
			return store.CompleteWithOutcome(ctx, id, "refusalwar", sc, OutcomeResult{})
		},
	} {
		sc := "scenario_" + map[string]string{"marked completed": "001", "completed with an outcome": "000"}[name]
		if err := store.SaveMekBayOperation(ctx, id, "refusalwar", sc, op); err != nil {
			t.Fatal(err)
		}
		if err := complete(sc); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, ok, _ := store.MekBayOperation(ctx, id, "refusalwar", sc); ok {
			t.Errorf("%s: the battle is over once the mission is", name)
		}
	}
}

func TestCrewIsDead(t *testing.T) {
	for _, c := range []struct {
		state, hits int
		dead        bool
	}{{0, 0, false}, {0, 5, false}, {1, 3, false}, {3, 0, false}, {5, 2, false}, {2, 0, true}, {4, 0, true}, {0, 6, true}, {1, 7, true}} {
		if got := crewIsDead(c.state, c.hits); got != c.dead {
			t.Errorf("state %d, %d hits: dead = %v, want %v", c.state, c.hits, got, c.dead)
		}
	}
	reply := func(crew string) string {
		return `{"action":"forceData","data":{"name":"F","timestamp":"t","groups":[{"name":"G","units":[{"id":"u1","unit":"BMKoshi_E","state":{"crew":[` + crew + `]}}]}]}}`
	}
	for name, c := range map[string]struct {
		crew string
		dead bool
	}{
		"healthy":      {`{"name":"Alrek","gunnerySkill":3,"pilotingSkill":4,"hits":1,"state":0}`, false},
		"state dead":   {`{"name":"Alrek","gunnerySkill":3,"pilotingSkill":4,"hits":0,"state":2}`, true},
		"six hits":     {`{"name":"Alrek","gunnerySkill":3,"pilotingSkill":4,"hits":6,"state":0}`, true},
		"killed":       {`{"name":"Alrek","gunnerySkill":3,"pilotingSkill":4,"hits":0,"state":4}`, true},
		"no crew data": {``, false},
	} {
		h, err := parseHangarForce(json.RawMessage(reply(c.crew)))
		if err != nil || len(h.Units) != 1 || h.Units[0].PilotDead != c.dead {
			t.Errorf("%s: %+v %v", name, h.Units, err)
		}
		if v := hangarViews(h)[0]; v.PilotDead != c.dead {
			t.Errorf("%s: the view carries it: %+v", name, v)
		}
	}
}

// MekBay counts a pilot dead when the cockpit is gone, whatever the crew's stored state: the cockpit slot destroyed, or the head with no structure left.
func TestPilotDeadFromTheCockpit(t *testing.T) {
	reply := func(state string) string {
		return `{"action":"forceData","data":{"name":"F","timestamp":"t","groups":[{"name":"G","units":[{"id":"u1","unit":"BMKoshi_E","state":{` + state + `,"crew":[{"name":"Alrek","gunnerySkill":3,"pilotingSkill":4,"hits":1,"state":0}]}}]}]}}`
	}
	for name, c := range map[string]struct {
		state string
		dead  bool
	}{
		"nothing":        {`"x":1`, false},
		"cockpit crit":   {`"crits":[{"name":"Cockpit","loc":"HD","slot":2,"destroyed":1}]`, true},
		"cockpit intact": {`"crits":[{"name":"Cockpit","loc":"HD","slot":2}]`, false},
		"command":        {`"crits":[{"name":"Command Console","loc":"HD","slot":2,"destroyed":1}]`, false},
		"head gone":      {`"locations":{"HD":{"armor":9,"internal":3}}`, true},
		"head hurt":      {`"locations":{"HD":{"armor":9,"internal":2}}`, false},
	} {
		h, err := parseHangarForce(json.RawMessage(reply(c.state)))
		if err != nil || len(h.Units) != 1 || h.Units[0].PilotDead != c.dead {
			t.Errorf("%s: %+v %v", name, h.Units, err)
		}
	}
}
