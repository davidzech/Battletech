package site

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/pilot"
	"github.com/davidzech/Battletech/internal/storage"
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
	var alrek pilot.NamedPilot
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

	// Scenario 001 is locked until 000 is completed: its page disables Deploy force and the server refuses the battle.
	if page := body(t, srv, "/refusalwar/scenario_001/", cookie); !strings.Contains(page, `id="deploy-open" disabled title="This mission isn`) {
		t.Error("a locked mission's Deploy force button should be disabled")
	}
	if got := start(srv.URL, two...).StatusCode; got != http.StatusConflict || len(fake.battles) != 0 {
		t.Errorf("locked mission: %d, want 409", got)
	}
	if _, err := store.SetScenarioCompleted(ctx, id, "refusalwar", "scenario_000", true); err != nil {
		t.Fatal(err)
	}

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
	if req.Account != mekbay.MekBayUID(seed, mekbay.SlotHangar) || req.Provided || req.HangarInstance != run.HangarInstance || len(req.Players) != 2 || req.OperationName != "Refusal War - Scenario 001" {
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
	// Alrek is the only named pilot deployed, so he commands the force: his mech is the commander's, and Player 2's force has none.
	if req.Players[0].Commander != "ua" || req.Players[1].Commander != "" {
		t.Errorf("the commander's mech: %q, %q", req.Players[0].Commander, req.Players[1].Commander)
	}

	// The answer is one link for the whole table: a mekbay-<id> host of its own opening the operation, never showing the account.
	m := battleURL.FindStringSubmatch(first.URL)
	if m == nil || m[3] != first.Operation {
		t.Fatalf("the link opens the operation on a mekbay-<id> host: %q (%s)", first.URL, first.Operation)
	}
	if strings.Contains(first.URL, mekbay.MekBayUID(seed, mekbay.SlotHangar)) {
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

	// While the battle is in progress the campaign is locked: starting it again, or any other change, is refused until it is completed or aborted.
	locked := func(name string, res *http.Response) {
		t.Helper()
		if b, _ := io.ReadAll(res.Body); res.StatusCode != http.StatusConflict || !strings.Contains(string(b), "Scenario 001: Pyre Light is in progress") {
			t.Errorf("%s during the battle: %d %s", name, res.StatusCode, b)
		}
	}
	locked("starting it again", start(srv.URL, two...))
	if len(fake.battles) != 1 {
		t.Errorf("a refused start never reaches MekBay: %d", len(fake.battles))
	}
	form := func(kv ...string) url.Values {
		v := url.Values{"campaign": {"refusalwar"}}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Add(kv[i], kv[i+1])
		}
		return v
	}
	pid := ""
	if ps, _ := store.Pilots(ctx, id, "refusalwar"); len(ps) > 0 {
		pid = ps[0].ID
	}
	for path, f := range map[string]url.Values{
		"/api/hangar/sell":                form("unit", "ub"),
		"/api/hangar/service":             form("kind", "rearm", "unit", "ua"),
		"/api/hangar/purchase":            form("unit", "BMGrizzly"),
		"/api/pilots":                     form("name", "Natasha"),
		"/api/pilots/allocate":            form("pilot", pid, "gunnery", "10"),
		"/api/pilots/rename":              form("pilot", pid, "name", "Ulric"),
		"/api/pilots/portrait":            form("pilot", pid, "portrait", "male_rank1_1.png"),
		"/api/pilots/fallen":              form("pilot", pid),
		"/api/warchest":                   form("funds", "1"),
		"/api/campaign/force-name":        form("name", "Iron Wolves"),
		"/api/campaign/players":           form("players", "1"),
		"/api/campaign/keywords":          form(),
		"/api/campaign/rollback":          form("checkpoint", "1"),
		"/api/campaign/scenario-complete": form("scenario", "scenario_000", "done", "0"),
		"/api/campaign/scenario-outcome":  form("scenario", "scenario_002", "inputs", "{}"),
	} {
		locked(path, post(t, srv, path, f, cookie, srv.URL))
	}
	// The battle's own mission can still be reported: that is how it ends.
	if res := post(t, srv, "/api/campaign/scenario-outcome", form("scenario", "scenario_001", "inputs", "{}"), cookie, srv.URL); res.StatusCode == http.StatusConflict {
		b, _ := io.ReadAll(res.Body)
		t.Errorf("previewing the battle's own outcome is allowed: %s", b)
	}

	// The pages say so: a banner everywhere but the battle's own page, the Manage page's controls disabled, and the other missions' buttons too.
	if page := body(t, srv, "/refusalwar/manage/", cookie); !strings.Contains(page, `id="battle-lock"`) || !strings.Contains(page, `<fieldset class="battle-lock-scope" disabled>`) || !strings.Contains(page, `href="/refusalwar/scenario_001/">Go to the mission</a>`) {
		t.Error("the Manage page is locked during the battle")
	}
	if page := body(t, srv, "/refusalwar/", cookie); !strings.Contains(page, `id="battle-lock"`) || !strings.Contains(page, `id="keywords-open" disabled`) || strings.Contains(page, `id="delete-open" disabled`) {
		t.Error("the campaign page is locked during the battle, except Delete campaign")
	}
	if page := body(t, srv, "/refusalwar/scenario_001/", cookie); strings.Contains(page, `id="battle-lock"`) {
		t.Error("the battle's own page has its own controls, not the lock banner")
	}
	if page := body(t, srv, "/refusalwar/scenario_000/", cookie); !strings.Contains(page, `id="battle-lock"`) {
		t.Error("another mission's page shows the lock")
	}

	// Aborting unlocks it. MekBay failing to start the next battle leaves nothing recorded and says why.
	if r := post(t, srv, "/api/battle/abort", form("scenario", "scenario_001"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("abort: %d", r.StatusCode)
	}
	fake.mu.Lock()
	fake.battleErr = errors.New("MekBay says that account doesn't own this force")
	fake.mu.Unlock()
	res := start(srv.URL, two...)
	if b, _ := io.ReadAll(res.Body); res.StatusCode != http.StatusBadGateway || !strings.Contains(string(b), "doesn't own") {
		t.Errorf("a MekBay failure is a 502 with the reason: %d %s", res.StatusCode, b)
	}
	if _, ok, _ := store.MekBayOperation(ctx, id, "refusalwar", "scenario_001"); ok {
		t.Error("a failed start records nothing")
	}
	fake.mu.Lock()
	fake.battleErr = nil
	fake.mu.Unlock()
	readBattle(t, start(srv.URL, two...))

	// Deleting the campaign is allowed during a battle, and takes the record with it.
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
	levels := map[string]pilot.NamedPilot{}
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

// A battle built in MekBay that then can't be recorded here is deleted there again (nothing would remember it), and the mission can be started again.
func TestABattleThatCantBeRecordedIsDeleted(t *testing.T) {
	const seed = "wolfpack2026"
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {seed}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	units := []string{providedPrefix + "bmblackhawk_prime", providedPrefix + "bmryoken_prime"}
	form := url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_000"}, "players": {"1"}, "p1_units": units, "p1_assign_unit": units, "p1_assign_pilot": {"Alrek", "Katarina"}}
	// The database refuses to record a battle (SQLite, which these tests run on).
	if _, err := store.DB().ExecContext(ctx, `CREATE TRIGGER refuse_battles BEFORE INSERT ON mekbay_operations BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatal(err)
	}
	res := post(t, srv, "/api/battle/start", form, cookie, srv.URL)
	if msg, _ := io.ReadAll(res.Body); res.StatusCode != http.StatusInternalServerError || strings.Contains(string(msg), "refused") {
		t.Fatalf("a battle that can't be recorded: %d %s", res.StatusCode, msg)
	}
	fake.mu.Lock()
	built, deleted := len(fake.battles), len(fake.battleDeletes)
	fake.mu.Unlock()
	if built != 1 || deleted != 1 {
		t.Fatalf("the battle built in MekBay is deleted there: %d built, %d deleted", built, deleted)
	}
	id := playthroughID(seed)
	if _, had, _ := store.MekBayOperation(ctx, id, "refusalwar", "scenario_000"); had {
		t.Error("no battle is in progress")
	}
	if h, _ := store.History(ctx, id, "refusalwar"); strings.HasPrefix(h[len(h)-1].Text, "Deployed") {
		t.Errorf("a battle that was not started is not in the history: %+v", h[len(h)-1])
	}
	// Once the database is well again the mission starts: the failed start left no claim behind.
	if _, err := store.DB().ExecContext(ctx, `DROP TRIGGER refuse_battles`); err != nil {
		t.Fatal(err)
	}
	readBattle(t, post(t, srv, "/api/battle/start", form, cookie, srv.URL))
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
	if _, err := store.SetScenarioCompleted(ctx, id, "refusalwar", "scenario_000", true); err != nil { // unlocks 001
		t.Fatal(err)
	}
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
	hurt.Units[0].Locations = map[string]mekbay.LocationLoss{"CT": {Armor: 6}}
	hurt.Units[1].Pilot, hurt.Units[1].CockpitGone = "Katarina", true // her cockpit is gone in this battle: the record sheet shows her dead
	second := hangarOf("BMGrizzly")
	second.Name = "Wolf Pack - Player 2"
	fake.mu.Lock()
	fake.status = mekbay.BattleStatus{Forces: []mekbay.BattleForce{{Alignment: "friendly", Force: hurt}, {Alignment: "friendly", Force: second}}}
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
	if f := got.Forces[0]; f.Units[0].PilotDead || !f.Units[1].PilotDead {
		t.Errorf("a pilot whose cockpit is gone in the battle is dead, the other not: %+v", f.Units)
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
	op := storage.MekBayOperation{OpForInstance: "of", OperationID: "op"}
	for name, complete := range map[string]func(scenario string) error{
		"marked completed": func(sc string) error {
			_, err := store.SetScenarioCompleted(ctx, id, "refusalwar", sc, true)
			return err
		},
		"completed with an outcome": func(sc string) error {
			return store.CompleteWithOutcome(ctx, id, "refusalwar", sc, content.OutcomeResult{})
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
		if got := mekbay.CrewIsDead(c.state, c.hits); got != c.dead {
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
		h, err := mekbay.ParseHangarForce(json.RawMessage(reply(c.crew)))
		if err != nil || len(h.Units) != 1 || h.Units[0].CrewDead != c.dead {
			t.Errorf("%s: %+v %v", name, h.Units, err)
		}
		if v := hangarViews(h)[0]; v.PilotDead != c.dead {
			t.Errorf("%s: the view carries it: %+v", name, v)
		}
		if v := battleViews(h)[0]; v.PilotDead != c.dead {
			t.Errorf("%s: the battle's view carries it: %+v", name, v)
		}
	}
}

// A provided omnimech reconfigured on the Deploy wizard's Configure step goes into battle as its chosen variant; a variant Clan Wolf doesn't field is refused.
func TestStartBattleWithReconfiguredMechs(t *testing.T) {
	const seed = "wolfpack2026"
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, _ := newMekBayTestServer(t, fake)
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {seed}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	fielded, not := swapChoices(t, "BMBlackHawk_Prime")
	units := []string{providedPrefix + "bmblackhawk_prime", providedPrefix + "bmryoken_prime"}
	form := func(to string) url.Values {
		return url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_000"}, "players": {"1"},
			"p1_units": units, "p1_assign_unit": units, "p1_assign_pilot": {"Alrek", "Katarina"},
			"variant_unit": {units[0]}, "variant_to": {to}}
	}
	if r := post(t, srv, "/api/deploy/plan", form(not), cookie, srv.URL); r.StatusCode != http.StatusBadRequest {
		t.Errorf("a variant Clan Wolf doesn't field: %d", r.StatusCode)
	}
	readBattle(t, post(t, srv, "/api/battle/start", form(fielded), cookie, srv.URL))
	if len(fake.battles) != 1 {
		t.Fatalf("one battle: %+v", fake.battles)
	}
	d := fake.battles[0].Players[0].Draft.Groups[0].Units
	cat, _ := catalog.ByName(fielded)
	if d[0].Unit != fielded || d[0].Pilot != "Alrek" || d[0].BV != cat.BV || d[1].Unit != "BMRyoken_Prime" {
		t.Errorf("the Nova goes as %s with its own BV, the Stormcrow as it was: %+v", fielded, d)
	}
}

// TestBattleLockIgnoresLeftoversAndHonoursClaims: a battle row left behind for a completed scenario, or for a scenario the campaign no longer has, doesn't lock
// the campaign (it would have no way out); a start in progress (a claim) does, until it is released or expires, and a second claim waits for the first.
func TestBattleLockIgnoresLeftoversAndHonoursClaims(t *testing.T) {
	srv, store, cookie := startedCompany(t)
	ctx := context.Background()
	id := playthroughID("wolfpack2026")
	ps, _ := store.Pilots(ctx, id, "refusalwar")
	rename := func(name string) int {
		return post(t, srv, "/api/pilots/rename", url.Values{"campaign": {"refusalwar"}, "pilot": {ps[0].ID}, "name": {name}}, cookie, srv.URL).StatusCode
	}

	// Leftovers: a completed scenario's battle and an unknown scenario's battle.
	if _, err := store.SetScenarioCompleted(ctx, id, "refusalwar", "scenario_000", true); err != nil {
		t.Fatal(err)
	}
	for _, sc := range []string{"scenario_000", "scenario_999"} {
		if err := store.SaveMekBayOperation(ctx, id, "refusalwar", sc, storage.MekBayOperation{OpForInstance: "o", OperationID: "op-" + sc}); err != nil {
			t.Fatal(err)
		}
	}
	if got := rename("Ulric"); got != http.StatusNoContent {
		t.Errorf("leftover battle rows lock the campaign: %d", got)
	}
	if page := body(t, srv, "/refusalwar/manage/", cookie); strings.Contains(page, `id="battle-lock"`) {
		t.Error("leftover battle rows show the lock banner")
	}

	// A claim locks it like a battle, and only one claim holds at a time.
	if ok, err := store.ClaimBattle(ctx, id, "refusalwar", "scenario_001"); err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if ok, _ := store.ClaimBattle(ctx, id, "refusalwar", "scenario_002"); ok {
		t.Error("a second claim went ahead while the first held")
	}
	if got := rename("Alrek"); got != http.StatusConflict {
		t.Errorf("a change during a claimed start: %d", got)
	}
	if page := body(t, srv, "/refusalwar/manage/", cookie); !strings.Contains(page, `id="battle-lock"`) || !strings.Contains(page, "Scenario 001") {
		t.Error("a claimed start shows the lock banner")
	}
	if err := store.ReleaseBattleClaim(ctx, id, "refusalwar"); err != nil {
		t.Fatal(err)
	}
	if got := rename("Alrek"); got != http.StatusNoContent {
		t.Errorf("after the release: %d", got)
	}

	// A claim older than its TTL (a server that died mid-start) no longer holds.
	if _, err := store.DB().ExecContext(ctx, store.Rebind(`UPDATE campaign_runs SET battle_claim = ?, battle_claim_scenario = 'scenario_001' WHERE playthrough_id = ?`),
		time.Now().Add(-storage.BattleClaimTTL-time.Minute).Unix(), id); err != nil {
		t.Fatal(err)
	}
	if got := rename("Ulric"); got != http.StatusNoContent {
		t.Errorf("an expired claim still locks: %d", got)
	}
	if ok, _ := store.ClaimBattle(ctx, id, "refusalwar", "scenario_001"); !ok {
		t.Error("an expired claim can be taken over")
	}
}

// Aborting a battle is guarded like every other change to a campaign: its fields come from the posted form only, and the form is bounded.
func TestAbortBattleGuards(t *testing.T) {
	srv, store, cookie := startedCompany(t)
	ctx := context.Background()
	id := playthroughID("wolfpack2026")
	if err := store.SaveMekBayOperation(ctx, id, "refusalwar", "scenario_001", storage.MekBayOperation{OperationID: "op-1"}); err != nil {
		t.Fatal(err)
	}
	inProgress := func() bool {
		t.Helper()
		_, ok, err := store.MekBayOperation(ctx, id, "refusalwar", "scenario_001")
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	form := url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_001"}}

	// The campaign and scenario are not read from the address.
	if r := post(t, srv, "/api/battle/abort?"+form.Encode(), url.Values{}, cookie, srv.URL); r.StatusCode != http.StatusNotFound || !inProgress() {
		t.Errorf("fields in the query string are ignored: %d, battle in progress %v", r.StatusCode, inProgress())
	}
	if r := post(t, srv, "/api/battle/abort?scenario=scenario_001", url.Values{"campaign": {"refusalwar"}}, cookie, srv.URL); r.StatusCode != http.StatusNotFound || !inProgress() {
		t.Errorf("a scenario in the query string is ignored: %d", r.StatusCode)
	}
	// A body past the limit is refused before anything is read from it.
	big := url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_001"}, "pad": {strings.Repeat("x", 70<<10)}}
	if r := post(t, srv, "/api/battle/abort", big, cookie, srv.URL); r.StatusCode != http.StatusBadRequest || !inProgress() {
		t.Errorf("an oversized form is refused: %d", r.StatusCode)
	}
	for name, r := range map[string]*http.Response{
		"another origin":   post(t, srv, "/api/battle/abort", form, cookie, "https://evil.example"),
		"no seed":          post(t, srv, "/api/battle/abort", form, nil, srv.URL),
		"an unknown seed":  post(t, srv, "/api/battle/abort", form, &http.Cookie{Name: "seed", Value: "nobodyatall"}, srv.URL),
		"unknown scenario": post(t, srv, "/api/battle/abort", url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_999"}}, cookie, srv.URL),
	} {
		if r.StatusCode < 400 || !inProgress() {
			t.Errorf("%s is refused: %d", name, r.StatusCode)
		}
	}
	// The posted form still aborts.
	if r := post(t, srv, "/api/battle/abort", form, cookie, srv.URL); r.StatusCode != http.StatusNoContent || inProgress() {
		t.Errorf("a proper request aborts the battle: %d, battle in progress %v", r.StatusCode, inProgress())
	}
}

// An abort that fails (here the rollback can't restore the hangar) leaves the battle as it was, in MekBay too: the campaign is still locked on it, so the
// mission can still be completed, or the abort tried again.
func TestFailedAbortKeepsTheBattleInMekBay(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	const seed = "wolfpack2026"
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {seed}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	id := playthroughID(seed)
	if err := store.SaveMekBayOperation(ctx, id, "refusalwar", "scenario_000", storage.MekBayOperation{OpForInstance: "opfor", OperationID: "op-stuck"}); err != nil {
		t.Fatal(err)
	}
	abort := func() int {
		return post(t, srv, "/api/battle/abort", url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_000"}, "mode": {"rollback"}}, cookie, srv.URL).StatusCode
	}
	deleted := func() []string {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return slices.Clone(fake.battleDeletes)
	}
	fake.mu.Lock()
	fake.copyErr = errors.New("mekbay is down")
	fake.mu.Unlock()
	if got := abort(); got != http.StatusBadGateway {
		t.Fatalf("an abort whose rollback fails: %d", got)
	}
	if _, had, _ := store.MekBayOperation(ctx, id, "refusalwar", "scenario_000"); !had {
		t.Error("a failed abort leaves the battle recorded")
	}
	if d := deleted(); len(d) != 0 {
		t.Errorf("a failed abort deleted the battle in MekBay: %v", d)
	}
	// Tried again once MekBay answers, the abort goes through and cleans up.
	fake.mu.Lock()
	fake.copyErr = nil
	fake.mu.Unlock()
	if got := abort(); got != http.StatusNoContent {
		t.Fatalf("the abort tried again: %d", got)
	}
	if d := deleted(); !slices.Contains(d, "op-stuck") {
		t.Errorf("the abort that went through cleans up in MekBay: %v", d)
	}
}
