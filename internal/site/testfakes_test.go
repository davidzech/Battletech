package site

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/storage"
)

func hangarFixture() mekbay.HangarForce {
	return mekbay.HangarForce{Name: "Test Hangar", Timestamp: "t", Units: []mekbay.HangarUnit{
		{ID: "u-vulture", Unit: "BMVulture_Prime", Group: "Hangar", Pilot: "Anya", Gunnery: 3, Piloting: 4}, // a skilled pilot: still base BV
		{ID: "u-loki1", Unit: "BMLoki_Prime", Group: "Deployed", Gunnery: 4, Piloting: 5, Damaged: true},
		{ID: "u-loki2", Unit: "BMLoki_Prime", Group: "Hangar", Gunnery: 4, Piloting: 5},
		{ID: "u-loki3", Unit: "BMLoki_Prime", Group: "Hangar", Gunnery: 4, Piloting: 5},
		{ID: "u-loki4", Unit: "BMLoki_Prime", Group: "Hangar", Gunnery: 4, Piloting: 5},
		{ID: "u-loki5", Unit: "BMLoki_Prime", Group: "Hangar", Gunnery: 4, Piloting: 5},
		{ID: "u-dead", Unit: "BMGrizzly", Group: "Hangar", Gunnery: 4, Piloting: 5, Destroyed: true},
		{ID: "u-odd", Unit: "BMNoSuchMech_X", Group: "Hangar", Gunnery: 4, Piloting: 5},
	}}
}

// fakeMekBay answers a fixed force for every hangar link and records battle requests instead of calling mekbay.com. Campaign hangars are in the database: tests
// set and read them with setRunHangar and runHangar.
type fakeMekBay struct {
	mu     sync.Mutex
	hangar mekbay.HangarForce // the force every ReadForce answers (unless forces has the instance)
	err    error              // ReadForce fails with it
	forces map[string][]byte  // forces by instance, as saved data (the import's tests)
	reads  []string           // the instances ReadForce was asked for
	// battles records Start battle requests; battleErr makes them fail.
	battles       []mekbay.BattleRequest
	battleErr     error
	status        mekbay.BattleStatus // what ReadBattle answers
	statusErr     error
	statusOps     []string                  // the operations ReadBattle was asked for
	battleUnits   map[string]map[string]any // what ReadBattleUnits answers: the battle's units by ID
	unitsErr      error                     // ReadBattleUnits fails with it
	unitReads     []string                  // the operations ReadBattleUnits was asked for
	unitSkips     [][]string                // and the forces it was told to skip
	battleDeletes []string
}

func (f *fakeMekBay) ReadForce(_ context.Context, instance string) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, instance)
	if f.err != nil {
		return nil, f.err
	}
	if data, ok := f.forces[instance]; ok {
		return data, nil
	}
	return forceOf(f.hangar), nil
}

func (f *fakeMekBay) StartBattle(_ context.Context, req mekbay.BattleRequest) (mekbay.BattleResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.battles = append(f.battles, req)
	if f.battleErr != nil {
		return mekbay.BattleResult{}, f.battleErr
	}
	res := mekbay.BattleResult{OperationID: req.OperationID, OpForInstance: req.OpFor.Reuse}
	if res.OperationID == "" {
		res.OperationID = mekbay.NewID()
	}
	if res.OpForInstance == "" {
		res.OpForInstance = mekbay.NewID()
	}
	if len(req.Allies.Groups) > 0 {
		res.AlliesInstance = mekbay.NewID()
	}
	for range req.Players {
		res.PlayerInstances = append(res.PlayerInstances, mekbay.NewID())
	}
	return res, nil
}

// ReadBattleUnits records what it was asked and answers battleUnits.
func (f *fakeMekBay) ReadBattleUnits(_ context.Context, _, operationID string, skip []string) (map[string]map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unitReads, f.unitSkips = append(f.unitReads, operationID), append(f.unitSkips, skip)
	if f.unitsErr != nil {
		return nil, f.unitsErr
	}
	return f.battleUnits, nil
}

func (f *fakeMekBay) DeleteBattle(_ context.Context, _ string, operationID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.battleDeletes = append(f.battleDeletes, operationID)
	return nil
}

func (f *fakeMekBay) ReadBattle(_ context.Context, operationID string) (mekbay.BattleStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statusOps = append(f.statusOps, operationID)
	return f.status, f.statusErr
}

// forceOf is a hangar as MekBay saves it (and the site stores it), built so that it reads back as h: groups, crews, damage, critical hits, spent ammunition, the
// destroyed flag and the torso check. A unit marked Damaged with no damage recorded gets one point of armor lost on its left arm; engine hits and spent
// ammunition with no slots of their own get slots in the center torso.
func forceOf(h mekbay.HangarForce) []byte {
	var groups []any
	byName := map[string]map[string]any{}
	for _, u := range h.Units {
		g := byName[u.Group]
		if g == nil {
			g = map[string]any{"id": "g-" + u.Group, "name": u.Group, "color": "", "units": []any{}}
			byName[u.Group] = g
			groups = append(groups, g)
		}
		crewState := 0
		if u.CrewDead {
			crewState = 2
		}
		locs := map[string]any{}
		for k, l := range u.Locations {
			locs[k] = map[string]any{"armor": l.Armor, "internal": l.Internal}
		}
		crits := []any{}
		engines := 0
		for _, c := range u.Crits {
			name := "Upper Arm Actuator"
			if c.Engine {
				name, engines = "Engine", engines+1
			}
			crits = append(crits, map[string]any{"name": name, "loc": c.Loc, "hits": 1})
		}
		for ; engines < u.EngineHits; engines++ {
			crits = append(crits, map[string]any{"name": "Engine", "loc": "CT", "hits": 1})
		}
		if u.Damaged && len(locs) == 0 && len(crits) == 0 {
			locs["LA"] = map[string]any{"armor": 1, "internal": 0}
		}
		if len(u.AmmoUsed2) > 0 {
			for _, a := range u.AmmoUsed2 {
				crits = append(crits, map[string]any{"name": a.Name, "loc": a.Loc, "consumed": a.Used})
			}
		} else {
			for i := 0; i < u.AmmoBins; i++ {
				used := u.AmmoUsed / u.AmmoBins
				if i == 0 {
					used += u.AmmoUsed % u.AmmoBins
				}
				crits = append(crits, map[string]any{"name": "Ammo", "loc": "CT", "consumed": max(used, 1)})
			}
		}
		if u.CockpitGone {
			crits = append(crits, map[string]any{"name": "Cockpit", "loc": "HD", "destroyed": 1})
		}
		state := map[string]any{
			"crew":      []any{map[string]any{"id": 0, "name": u.Pilot, "gunnerySkill": u.Gunnery, "pilotingSkill": u.Piloting, "hits": 0, "state": crewState}},
			"crits":     crits,
			"locations": locs,
			"heat":      map[string]any{"current": 0, "previous": 0},
			"destroyed": u.Destroyed,
			"turnState": map[string]any{"applyMovePSR": false},
		}
		if u.TorsoCheckFailed {
			state["ruleChecks"] = map[string]any{"core.torso-crippling": map[string]any{"status": "failed"}}
		}
		g["units"] = append(g["units"].([]any), map[string]any{"id": u.ID, "unit": u.Unit, "alias": "", "state": state})
	}
	if groups == nil {
		groups = []any{}
	}
	b, err := json.Marshal(map[string]any{"version": 1, "instanceId": "hangar-" + h.Name, "type": "cbt", "name": h.Name, "timestamp": h.Timestamp, "groups": groups})
	if err != nil {
		panic(err)
	}
	return b
}

// battleUnitsOf is h's units as a battle's forces hold them in MekBay (what ReadBattleUnits answers), by ID.
func battleUnitsOf(h mekbay.HangarForce) map[string]map[string]any {
	var force struct {
		Groups []struct {
			Units []map[string]any `json:"units"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(forceOf(h), &force); err != nil {
		panic(err)
	}
	out := map[string]map[string]any{}
	for _, g := range force.Groups {
		for _, u := range g.Units {
			out[u["id"].(string)] = u
		}
	}
	return out
}

// setRunHangar stores h as the hangar of a started campaign of the seed's.
func setRunHangar(t *testing.T, store *storage.Store, seed, campaign string, h mekbay.HangarForce) {
	t.Helper()
	if err := store.SetHangar(context.Background(), playthroughID(seed), campaign, forceOf(h)); err != nil {
		t.Fatal(err)
	}
}

// runHangar is the hangar of a started campaign of the seed's, as the site reads it.
func runHangar(t *testing.T, store *storage.Store, seed, campaign string) mekbay.HangarForce {
	t.Helper()
	raw, err := store.Hangar(context.Background(), playthroughID(seed), campaign)
	if err != nil {
		t.Fatal(err)
	}
	h, err := mekbay.ParseForce(raw)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// runUnit is the unit with this ID in a started campaign's hangar (found is false if it is not there).
func runUnit(t *testing.T, store *storage.Store, seed, campaign, id string) (mekbay.HangarUnit, bool) {
	t.Helper()
	return hangarUnit(runHangar(t, store, seed, campaign), id)
}

func newMekBayTestServer(t *testing.T, mb mekbay.MekBay) (*httptest.Server, *storage.Store) {
	t.Helper()
	store, err := storage.OpenSQLite(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	srv := httptest.NewServer(NewHandler(plainScenario001Root(t), store, WithMekBay(mb)))
	t.Cleanup(srv.Close)
	return srv, store
}

func get(t *testing.T, srv *httptest.Server, path string, cookie *http.Cookie) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

// plainScenario001Root is a copy of the campaigns (without the images) in which Scenario 001 has no outcome engine, standing in for a scenario that is completed
// by a plain Mark completed. The real Scenario 001 follows the Aces model (see TestScenario001Outcome).
func plainScenario001Root(t *testing.T) string {
	t.Helper()
	return campaignsCopy(t, map[string]string{"RefusalWar/Scenario_001/Outcome.md": `---
id: scenario_001
default_next: ""
branches: [scenario_002, scenario_003]
---

# Scenario 001: Pyre Light - Outcome

## Victory & Rewards

A plain scenario for the tests.
`})
}

// campaignsCopy copies the campaigns into a temporary folder, without the scenarios' big images (a campaign's icon is kept), then writes the files in
// files (path relative to the campaigns folder -> text), replacing or adding them.
func campaignsCopy(t *testing.T, files map[string]string) string {
	t.Helper()
	src, dst := "../../campaigns", t.TempDir()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if d.IsDir() {
			if d.Name() == "images" && strings.Count(filepath.ToSlash(rel), "/") >= 2 { // a scenario's big images are not needed; a campaign's icon is
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	for rel, text := range files {
		p := filepath.Join(dst, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

// eventually waits for something the site does in the background (a clean-up in MekBay) to show in the fake, failing the test with what it last saw.
func eventually(t *testing.T, what string, done func() (bool, any)) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		ok, saw := done()
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("%s: never happened, last saw %v", what, saw)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
