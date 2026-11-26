package site

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/mekbay"
)

// Temper Edge gives the players allies of its own, the downed Mist Lynx and the Savior sent to repair it: Start battle builds them as a force after the players'
// on the friendly side, for free; the status marks that force as the allies; and completing the mission never writes its units into the hangar.
func TestScenarioAlliesJoinTheBattle(t *testing.T) {
	const seed = "wolfpack2026"
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	id := playthroughID(seed)
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {seed}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	for _, sc := range []string{"scenario_000", "scenario_001", "scenario_002"} { // Sudeten leads to Temper Edge
		if _, err := store.SetScenarioCompleted(ctx, id, "refusalwar", sc, true); err != nil {
			t.Fatal(err)
		}
	}
	page := body(t, srv, "/refusalwar/scenario_004/", cookie)
	if !strings.Contains(page, `id="deploy-allies"`) || !strings.Contains(page, "<b>Mist Lynx C</b> (3/2)") || !strings.Contains(page, "<b>Savior Repair Vehicle</b>") {
		t.Errorf("the Deploy section names the allies: %.300s", page)
	}

	readBattle(t, post(t, srv, "/api/battle/start", url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_004"}, "players": {"1"},
		"p1_units": {"ua"}, "p1_assign_unit": {"ua"}, "p1_assign_pilot": {"Alrek"}}, cookie, srv.URL))
	a := fake.battles[0].Allies
	if len(fake.battles[0].Players) != 1 || len(a.Groups) != 1 || len(a.Groups[0].Units) != 2 || a.Name == "" {
		t.Fatalf("one player, and the allies as a force of their own: %+v", a)
	}
	if u := a.Groups[0].Units[0]; u.Unit != "BMKoshi_C" || u.Pilot != "Vesna" || u.Gunnery != 3 || u.Piloting != 2 {
		t.Errorf("the Mist Lynx as the scenario gives it: %+v", u)
	}
	if u := a.Groups[0].Units[1]; u.Unit != "SVSaviorRepairVehicle" {
		t.Errorf("and the Savior: %+v", u)
	}
	op, _, _ := store.MekBayOperation(ctx, id, "refusalwar", "scenario_004")
	if op.AlliesInstance == "" {
		t.Fatalf("the battle records the allies' force: %+v", op)
	}

	// The status marks the allies' force, and only it.
	fake.mu.Lock()
	fake.status = mekbay.BattleStatus{Forces: []mekbay.BattleForce{
		{InstanceID: "p1", Alignment: "friendly", Force: hangarOf("BMKoshi_E")},
		{InstanceID: op.AlliesInstance, Alignment: "friendly", Force: hangarOf("BMKoshi_C", "SVSaviorRepairVehicle")},
	}}
	fake.mu.Unlock()
	res, err := srv.Client().Do(func() *http.Request {
		r, _ := http.NewRequest("GET", srv.URL+"/api/battle/status?campaign=refusalwar&scenario=scenario_004", nil)
		r.AddCookie(cookie)
		return r
	}())
	if err != nil {
		t.Fatal(err)
	}
	var st battleStatusResponse
	if err := json.NewDecoder(res.Body).Decode(&st); err != nil || len(st.Forces) != 2 || st.Forces[0].Allies || !st.Forces[1].Allies {
		t.Fatalf("the allies are marked in the status: %+v %v", st.Forces, err)
	}

	// Completing the mission writes the players' mechs back into the hangar, but skips the allies' force.
	b, _ := json.Marshal(map[string]any{"pilots": []map[string]any{}, "mission": map[string]any{"data_core": true, "mist_lynx": false, "falcon_mechs": 0}})
	if got := post(t, srv, "/api/campaign/scenario-outcome/commit", url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_004"}, "inputs": {string(b)}}, cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("commit: %d", got)
	}
	if len(fake.commits) != 1 || strings.Join(fake.commits[0].Skip, ",") != op.AlliesInstance {
		t.Errorf("the write-back leaves the allies out: %+v", fake.commits)
	}
}
