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
	onEdit     func() // called at the start of every hangar edit (say, to cancel the request while MekBay is being called)
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
	if len(req.Allies.Groups) > 0 {
		res.AlliesInstance = mekbay.NewID()
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

// The fake's edits save and return the hangar as it would be after them: a sale without the unit sold, a purchase with the units bought; repairs and swaps
// leave it as is.
func (f *fakeMekBay) edited(change func(h *mekbay.HangarForce)) (mekbay.HangarForce, error) {
	if f.onEdit != nil {
		f.onEdit()
	}
	if f.serviceErr != nil {
		return mekbay.HangarForce{}, f.serviceErr
	}
	h := f.hangar
	h.Units = append([]mekbay.HangarUnit(nil), f.hangar.Units...)
	if change != nil {
		change(&h)
	}
	f.hangar = h // MekBay keeps what was saved, so the next read or edit starts from it
	return h, nil
}

func (f *fakeMekBay) SellUnit(_ context.Context, req mekbay.SellRequest) (mekbay.HangarForce, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sales = append(f.sales, req)
	return f.edited(func(h *mekbay.HangarForce) {
		for i, u := range h.Units {
			if u.ID == req.UnitID {
				h.Units = append(h.Units[:i], h.Units[i+1:]...)
				break
			}
		}
	})
}

func (f *fakeMekBay) SwapUnit(_ context.Context, req mekbay.SwapRequest) (mekbay.HangarForce, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.swaps = append(f.swaps, req)
	return f.edited(nil)
}

func (f *fakeMekBay) ServiceUnit(_ context.Context, req mekbay.ServiceRequest) (mekbay.HangarForce, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.services = append(f.services, req)
	return f.edited(nil)
}

func (f *fakeMekBay) PurchaseUnits(_ context.Context, req mekbay.PurchaseRequest) (mekbay.HangarForce, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.purchases = append(f.purchases, req)
	return f.edited(func(h *mekbay.HangarForce) {
		for i, id := range req.Units {
			h.Units = append(h.Units, mekbay.HangarUnit{ID: fmt.Sprintf("bought-%d", i), Unit: id, Group: "Hangar", Gunnery: 4, Piloting: 5})
		}
	})
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
		if filepath.ToSlash(rel) == "RefusalWar/Scenario_001/Outcome.md" {
			data = []byte(`---
id: scenario_001
default_next: ""
branches: [scenario_002, scenario_003]
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
