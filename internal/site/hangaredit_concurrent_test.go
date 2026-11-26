package site

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/hangar"
	"github.com/davidzech/Battletech/internal/mekbay"
)

// readModifySaveMekBay is a fakeMekBay whose repairs behave like the real client's: the force is read, changed and saved whole, with time in between, so
// two repairs at once both start from the same force and the later save undoes the earlier one. A repair that finds nothing to repair on the force it
// read is mekbay.ErrNoChange, as in the real client.
type readModifySaveMekBay struct {
	*fakeMekBay
	meet chan struct{} // two repairs that are in MekBay at the same moment meet here, between their read and their save
}

func (f *readModifySaveMekBay) ServiceUnit(_ context.Context, req mekbay.ServiceRequest) (mekbay.HangarForce, error) {
	f.mu.Lock()
	f.services = append(f.services, req)
	h := f.hangar
	h.Units = slices.Clone(f.hangar.Units)
	f.mu.Unlock()
	select { // another repair reads the same force before this one saves, or none does for a long while
	case f.meet <- struct{}{}:
	case <-f.meet:
	case <-time.After(200 * time.Millisecond):
	}
	for i, u := range h.Units {
		if u.ID != req.UnitID {
			continue
		}
		if len(u.Locations) == 0 {
			return mekbay.HangarForce{}, mekbay.ErrNoChange
		}
		h.Units[i].Locations, h.Units[i].Damaged = nil, false
	}
	f.mu.Lock()
	f.hangar = h
	f.mu.Unlock()
	return h, nil
}

// Two paid edits of one run's hangar never run in MekBay at the same time, and one that then finds nothing left to do is refunded: the same repair asked for
// twice at once is charged once, and repairs of two mechs at once are both kept.
func TestConcurrentHangarEdits(t *testing.T) {
	fake := &readModifySaveMekBay{fakeMekBay: &fakeMekBay{hangar: startingHangar()}, meet: make(chan struct{})}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	if _, err := store.DB().Exec(`UPDATE campaign_runs SET warchest = 4500`); err != nil {
		t.Fatal(err)
	}
	id := playthroughID("wolfpack2026")
	funds := func() int { r, _, _ := store.Run(ctx, id, "refusalwar"); return r.Warchest }
	damaged := func() mekbay.HangarForce {
		h := hangarOf("BMUller_J", "BMDasher_Prime", "BMGrizzly")
		h.Units[0].Locations, h.Units[0].Damaged = map[string]mekbay.LocationLoss{"LA": {Armor: 3, Internal: 1}}, true
		h.Units[1].Locations, h.Units[1].Damaged = map[string]mekbay.LocationLoss{"RA": {Armor: 2, Internal: 1}}, true
		return h
	}
	// repairs asks for a structure repair of each unit named, all at once, and answers the statuses in order.
	repairs := func(units ...string) []int {
		out := make([]int, len(units))
		var wg sync.WaitGroup
		for i, unit := range units {
			wg.Add(1)
			go func() {
				defer wg.Done()
				form := url.Values{"campaign": {"refusalwar"}, "kind": {"repair"}, "unit": {unit}, "level": {mekbay.RepairStructure}}
				req, _ := http.NewRequest("POST", srv.URL+"/api/hangar/service", strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("Origin", srv.URL)
				req.AddCookie(cookie)
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Error(err)
					return
				}
				res.Body.Close()
				out[i] = res.StatusCode
			}()
		}
		wg.Wait()
		return out
	}
	repaired := func() int {
		h, err := store.History(ctx, id, "refusalwar")
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range h {
			if strings.HasPrefix(e.Text, "Repaired ") {
				n++
			}
		}
		return n
	}
	uller, _ := catalog.ByName("BMUller_J")
	dasher, _ := catalog.ByName("BMDasher_Prime")
	ullerCost := hangar.RepairCost(mekbay.RepairStructure, uller.Tons, uller.TechBase)
	dasherCost := hangar.RepairCost(mekbay.RepairStructure, dasher.Tons, dasher.TechBase)

	// The same repair twice at once (a double click, or two players at the table): made once, charged once, in the history once.
	fake.setHangar(damaged(), nil)
	got := repairs("ua", "ua")
	slices.Sort(got)
	if !slices.Equal(got, []int{http.StatusOK, http.StatusConflict}) {
		t.Errorf("the same repair twice at once: %v", got)
	}
	if funds() != 4500-ullerCost || repaired() != 1 {
		t.Errorf("the same repair twice at once left %d SP (want %d, charged once) and %d history lines", funds(), 4500-ullerCost, repaired())
	}

	// Two mechs repaired at once: both repairs are kept and both paid for.
	fake.setHangar(damaged(), nil)
	before := funds()
	if got := repairs("ua", "ub"); !slices.Equal(got, []int{http.StatusOK, http.StatusOK}) {
		t.Errorf("two mechs repaired at once: %v", got)
	}
	h, _ := fake.ReadHangar(ctx, "")
	if len(h.Units[0].Locations) != 0 || len(h.Units[1].Locations) != 0 {
		t.Errorf("one repair undid the other: %+v / %+v", h.Units[0].Locations, h.Units[1].Locations)
	}
	if funds() != before-ullerCost-dasherCost {
		t.Errorf("two repairs left %d SP, want %d", funds(), before-ullerCost-dasherCost)
	}
}
