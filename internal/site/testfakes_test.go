package site

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/davidzech/Battletech/internal/content"

	"github.com/davidzech/Battletech/internal/mekbay"

	"github.com/davidzech/Battletech/internal/catalog"
)

func TestLookupUnitAndParseMul(t *testing.T) {
	for _, c := range []struct{ chassis, model, name string }{
		{"Koshi (Mist Lynx)", "E", "BMKoshi_E"},
		{"Grizzly", "", "BMGrizzly"},
		{"Mad Cat (Timber Wolf)", "H", "BMMadCat_H"},
	} {
		if u, ok := catalog.Lookup(c.chassis, c.model); !ok || u.Name != c.name || u.BV <= 0 || u.Tons <= 0 || u.Class == "" {
			t.Errorf("lookupUnit(%q, %q) = %+v, %v; want %s", c.chassis, c.model, u, ok, c.name)
		}
	}
	if u, ok := catalog.ByName("BMVulture_Prime"); !ok || u.BV != 2351 || catalog.DisplayName(u) != "Mad Dog Prime" {
		t.Errorf("unitByName(BMVulture_Prime) = %+v, %v, display %q", u, ok, catalog.DisplayName(u))
	}
	if _, ok := catalog.Lookup("No Such Mech", "X"); ok {
		t.Error("unknown unit should not be found")
	}

	raw, err := os.ReadFile("../../campaigns/RefusalWar/Scenario_000/OpFor1.mul")
	if err != nil {
		t.Fatal(err)
	}
	es, err := content.ParseMul(raw)
	if err != nil || len(es) != 3 {
		t.Fatalf("parseMul = %v, %v", es, err)
	}
	if es[0].Chassis != "Koshi (Mist Lynx)" || es[0].Model != "E" || es[0].Pilot != "Tyra" || es[0].Gunnery != 3 || es[0].Piloting != 4 {
		t.Errorf("first entity = %+v", es[0])
	}
	if _, err := content.ParseMul([]byte("not xml")); err == nil {
		t.Error("garbage should not parse")
	}
}

func TestAdjustedBV(t *testing.T) {
	for _, c := range []struct{ base, g, p, want int }{
		{1000, 4, 5, 1000}, // the standard pilot
		{1000, 3, 4, 1320},
		{1000, 0, 0, 2420},
		{1000, 9, 9, 640}, // skills past 8 count as 8
		{2351, 4, 5, 2351},
	} {
		if got := catalog.AdjustedBV(c.base, c.g, c.p); got != c.want {
			t.Errorf("adjustedBV(%d, %d, %d) = %d, want %d", c.base, c.g, c.p, got, c.want)
		}
	}
}

func TestEveryScenarioForceResolves(t *testing.T) {
	lib := content.Library{Root: "../../campaigns"}
	cs, err := lib.Campaigns()
	if err != nil {
		t.Fatal(err)
	}
	for i := range cs {
		for j := range cs[i].Scenarios {
			sc := &cs[i].Scenarios[j]
			if len(sc.Forces) == 0 {
				continue
			}
			d, err := opForDraft(cs[i], sc)
			if err != nil {
				t.Errorf("%s/%s: %v", cs[i].Slug, sc.Slug, err)
				continue
			}
			if len(d.Groups) != len(sc.Forces) {
				t.Errorf("%s: %d groups for %d force files", sc.Slug, len(d.Groups), len(sc.Forces))
			}
		}
	}
}

func TestScenarioBVLimit(t *testing.T) {
	c, ok := (content.Library{Root: "../../campaigns"}).Campaign("refusalwar")
	if !ok {
		t.Fatal("no campaign")
	}
	if sc, _ := c.Scenario("scenario_001"); sc.BVLimit != 7500 {
		t.Errorf("scenario 001 BV limit = %d, want 7500", sc.BVLimit)
	}
	if sc, _ := c.Scenario("scenario_000"); sc.BVLimit != 0 {
		t.Errorf("scenario 000 should have no BV limit, got %d", sc.BVLimit)
	}
}

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

func TestHangarViewsAndDeployProblem(t *testing.T) {
	h := hangarFixture()
	views := hangarViews(h)
	if v := views[0]; v.Name != "Mad Dog" || v.Model != "Prime" || v.Icon != "/icons/vulture.png" || v.BV != 2351 || v.Pilot != "Anya" || v.Class != "Heavy" || v.Tons != 60 || v.InDeployed {
		t.Errorf("vulture view = %+v", v)
	}
	if v := views[1]; !v.InDeployed || !v.Damaged || v.BV <= 0 {
		t.Errorf("loki view = %+v", v)
	}
	if v := views[7]; !v.Unknown || v.Name != "BMNoSuchMech_X" {
		t.Errorf("unknown view = %+v", v)
	}
	for _, c := range []struct {
		ids   []string
		limit int
		want  string // substring of the problem, or "" for fine
	}{
		{[]string{"u-vulture", "u-loki1"}, 10000, ""},
		{[]string{"u-vulture", "u-loki1"}, 0, ""},
		{nil, 10000, "at least one"},
		{[]string{"u-vulture", "u-vulture"}, 0, "twice"},
		{[]string{"u-ghost"}, 0, "no longer in your hangar"},
		{[]string{"u-dead"}, 0, "destroyed"},
		{[]string{"u-odd"}, 0, "unit catalog"},
		{[]string{"u-loki1", "u-loki2", "u-loki3", "u-loki4", "u-loki5"}, 10000, "over this scenario's limit of 10000"},
		{[]string{"u-loki1", "u-loki2", "u-loki3", "u-loki4", "u-loki5"}, 0, ""},
	} {
		got := deployProblem(h, c.ids, c.limit)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("deployProblem(%v, %d) = %q, want it to contain %q", c.ids, c.limit, got, c.want)
		}
	}
}

// fakeMekBay serves a fixed hangar and records requests instead of calling mekbay.com.
type fakeMekBay struct {
	mu     sync.Mutex
	hangar mekbay.HangarForce
	err    error
	// services records repair and rearm requests; serviceErr makes them fail.
	services   []mekbay.ServiceRequest
	purchases  []mekbay.PurchaseRequest
	clones     []mekbay.CloneRequest
	created    []createdForce
	sales      []mekbay.SellRequest
	swaps      []mekbay.SwapRequest
	cloneErr   error
	serviceErr error
	// battles records Start battle requests; battleErr makes them fail.
	battles       []mekbay.BattleRequest
	battleErr     error
	status        mekbay.BattleStatus // what ReadBattle answers
	statusErr     error
	statusOps     []string // the operations ReadBattle was asked for
	commits       []mekbay.CommitBattleRequest
	copies        []mekbay.CopyRequest
	deletes       []string
	battleDeletes []string
	copyErr       error
	commitErr     error
}

func (f *fakeMekBay) ReadHangar(context.Context, string) (mekbay.HangarForce, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hangar, f.err
}

// CreateForce records the force it was asked to make and answers it as MekBay would read it back.
func (f *fakeMekBay) CreateForce(_ context.Context, owner, instance string, d mekbay.ForceDraft) (mekbay.HangarForce, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, createdForce{Owner: owner, Instance: instance, Draft: d})
	if f.cloneErr != nil {
		return mekbay.HangarForce{}, f.cloneErr
	}
	h := mekbay.HangarForce{Name: d.Name}
	for _, g := range d.Groups {
		for i, u := range g.Units {
			h.Units = append(h.Units, mekbay.HangarUnit{ID: fmt.Sprintf("%s-%d", instance[:8], i), Unit: u.Unit, Group: g.Name, Gunnery: u.Gunnery, Piloting: u.Piloting})
		}
	}
	return h, nil
}

// createdForce is one CreateForce call.
type createdForce struct {
	Owner, Instance string
	Draft           mekbay.ForceDraft
}

func (f *fakeMekBay) CloneHangar(_ context.Context, req mekbay.CloneRequest) (mekbay.HangarForce, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clones = append(f.clones, req)
	if f.cloneErr != nil {
		return mekbay.HangarForce{}, f.cloneErr
	}
	return f.hangar, f.err
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
	for range req.Players {
		res.PlayerInstances = append(res.PlayerInstances, mekbay.NewID())
	}
	return res, nil
}

// CommitBattle records what it was asked to write back.
func (f *fakeMekBay) CommitBattle(_ context.Context, req mekbay.CommitBattleRequest) (mekbay.CommitBattleReport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commits = append(f.commits, req)
	if f.commitErr != nil {
		return mekbay.CommitBattleReport{}, f.commitErr
	}
	return mekbay.CommitBattleReport{Matched: 1}, nil
}

// CopyForce and DeleteForce record what they were asked.
func (f *fakeMekBay) CopyForce(_ context.Context, req mekbay.CopyRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.copies = append(f.copies, req)
	return f.copyErr
}

func (f *fakeMekBay) DeleteForce(_ context.Context, _ string, instances ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, instances...)
	return nil
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

func (f *fakeMekBay) SellUnit(_ context.Context, req mekbay.SellRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sales = append(f.sales, req)
	return f.serviceErr
}

func (f *fakeMekBay) SwapUnit(_ context.Context, req mekbay.SwapRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.swaps = append(f.swaps, req)
	return f.serviceErr
}

func (f *fakeMekBay) ServiceUnit(_ context.Context, req mekbay.ServiceRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.services = append(f.services, req)
	return f.serviceErr
}

func (f *fakeMekBay) PurchaseUnits(_ context.Context, req mekbay.PurchaseRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.purchases = append(f.purchases, req)
	return f.serviceErr
}

func newMekBayTestServer(t *testing.T, mb mekbay.MekBay) (*httptest.Server, *Store) {
	t.Helper()
	store, err := OpenSQLite(t.TempDir() + "/test.db")
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
	src, dst := "../../campaigns", t.TempDir()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if d.IsDir() {
			if d.Name() == "images" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if filepath.ToSlash(rel) == "RefusalWar/Scenario_001/Outcome.md" {
			data = []byte(`---
id: scenario_001
default_next: ""
---

# Scenario 001: Pyre Light - Outcome

## Victory & Rewards

A plain scenario for the tests.
`)
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}
