package site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/hangar"
	"github.com/davidzech/Battletech/internal/mekbay"
)

// Two paid edits of one run's hangar take turns (each is one transaction holding the run's lock), and one that then finds nothing left to do is refused and
// charges nothing: the same repair asked for twice at once is charged once, and repairs of two mechs at once are both kept.
func TestConcurrentHangarEdits(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
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
	setRunHangar(t, store, "wolfpack2026", "refusalwar", damaged())
	got := repairs("ua", "ua")
	slices.Sort(got)
	if !slices.Equal(got, []int{http.StatusOK, http.StatusConflict}) {
		t.Errorf("the same repair twice at once: %v", got)
	}
	if funds() != 4500-ullerCost || repaired() != 1 {
		t.Errorf("the same repair twice at once left %d SP (want %d, charged once) and %d history lines", funds(), 4500-ullerCost, repaired())
	}

	// Two mechs repaired at once: both repairs are kept and both paid for.
	setRunHangar(t, store, "wolfpack2026", "refusalwar", damaged())
	before := funds()
	if got := repairs("ua", "ub"); !slices.Equal(got, []int{http.StatusOK, http.StatusOK}) {
		t.Errorf("two mechs repaired at once: %v", got)
	}
	h := runHangar(t, store, "wolfpack2026", "refusalwar")
	if len(h.Units[0].Locations) != 0 || len(h.Units[1].Locations) != 0 {
		t.Errorf("one repair undid the other: %+v / %+v", h.Units[0].Locations, h.Units[1].Locations)
	}
	if funds() != before-ullerCost-dasherCost {
		t.Errorf("two repairs left %d SP, want %d", funds(), before-ullerCost-dasherCost)
	}
}

// Two servers sharing one database (several instances of the site) never undo each other's edits to one run's hangar either: the run's lock is the database's.
func TestConcurrentHangarEditsAcrossServers(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srvA, store := newMekBayTestServer(t, fake)
	srvB := httptest.NewServer(NewHandler(plainScenario001Root(t), store, WithMekBay(fake)))
	t.Cleanup(srvB.Close)
	cookie := post(t, srvA, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	post(t, srvA, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srvA.URL)
	if r := post(t, srvA, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srvA.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	if _, err := store.DB().Exec(`UPDATE campaign_runs SET warchest = 4500`); err != nil {
		t.Fatal(err)
	}
	h := hangarOf("BMUller_J", "BMDasher_Prime", "BMGrizzly")
	h.Units[0].Locations, h.Units[0].Damaged = map[string]mekbay.LocationLoss{"LA": {Armor: 3, Internal: 1}}, true
	h.Units[1].Locations, h.Units[1].Damaged = map[string]mekbay.LocationLoss{"RA": {Armor: 2, Internal: 1}}, true
	setRunHangar(t, store, "wolfpack2026", "refusalwar", h)

	var wg sync.WaitGroup
	got := make([]int, 2)
	for i, srv := range []*httptest.Server{srvA, srvB} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i] = repairStatus(t, srv, cookie, []string{"ua", "ub"}[i])
		}()
	}
	wg.Wait()
	if !slices.Equal(got, []int{http.StatusOK, http.StatusOK}) {
		t.Errorf("two mechs repaired at once on two servers: %v", got)
	}
	after := runHangar(t, store, "wolfpack2026", "refusalwar")
	if len(after.Units[0].Locations) != 0 || len(after.Units[1].Locations) != 0 {
		t.Errorf("one server's repair undid the other's: %+v / %+v", after.Units[0].Locations, after.Units[1].Locations)
	}
}

// repairStatus asks srv for a structure repair of the unit and answers the status.
func repairStatus(t *testing.T, srv *httptest.Server, cookie *http.Cookie, unit string) int {
	form := url.Values{"campaign": {"refusalwar"}, "kind": {"repair"}, "unit": {unit}, "level": {mekbay.RepairStructure}}
	req, _ := http.NewRequest("POST", srv.URL+"/api/hangar/service", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", srv.URL)
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Error(err)
		return 0
	}
	res.Body.Close()
	return res.StatusCode
}
