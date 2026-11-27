package site

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/storage"
)

// startedWith4500 starts Refusal War for the seed wolfpack2026 on a test server, with 4,500 SP in the warchest (the hangar tests are about prices, not the
// campaign's starting funds).
func startedWith4500(t *testing.T) (*httptest.Server, *storage.Store, *fakeMekBay, *http.Cookie) {
	t.Helper()
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	if _, err := store.DB().Exec(`UPDATE campaign_runs SET warchest = 4500`); err != nil {
		t.Fatal(err)
	}
	return srv, store, fake, cookie
}

// A change made to the hangar (here a sale and a purchase) is in the hangar the next page shows; a refused one changes neither the hangar nor the warchest.
func TestHangarChangesAreStored(t *testing.T) {
	srv, store, fake, cookie := startedWith4500(t)
	units := func() map[string]string {
		out := map[string]string{}
		for _, u := range runHangar(t, store, "wolfpack2026", "refusalwar").Units {
			out[u.ID] = u.Unit
		}
		return out
	}
	funds := func() int {
		r, _, _ := store.Run(t.Context(), playthroughID("wolfpack2026"), "refusalwar")
		return r.Warchest
	}
	if units()["ua"] == "" {
		t.Fatal("the started campaign's hangar holds ua")
	}
	if got := post(t, srv, "/api/hangar/sell", url.Values{"campaign": {"refusalwar"}, "unit": {"ua"}}, cookie, srv.URL).StatusCode; got != http.StatusOK {
		t.Fatalf("sell: %d", got)
	}
	if units()["ua"] != "" {
		t.Error("after the sale the hangar no longer holds the unit sold")
	}
	if got := post(t, srv, "/api/hangar/purchase", url.Values{"campaign": {"refusalwar"}, "unit": {"BMGrizzly"}}, cookie, srv.URL).StatusCode; got != http.StatusOK {
		t.Fatalf("buy: %d", got)
	}
	u := units()
	grizzlies := 0
	for _, design := range u {
		if design == "BMGrizzly" {
			grizzlies++
		}
	}
	if len(u) != 5 || grizzlies != 2 {
		t.Errorf("after the purchase the hangar holds the unit bought: %v", u)
	}
	if p := body(t, srv, "/refusalwar/manage/", cookie); strings.Count(p, "Grizzly") < 2 {
		t.Error("the Manage page shows the hangar as it is now")
	}
	// A purchase the warchest can't cover changes nothing.
	before, left := units(), funds()
	if _, err := store.DB().Exec(`UPDATE campaign_runs SET warchest = 10`); err != nil {
		t.Fatal(err)
	}
	if got := post(t, srv, "/api/hangar/purchase", url.Values{"campaign": {"refusalwar"}, "unit": {"BMGrizzly"}}, cookie, srv.URL).StatusCode; got != http.StatusConflict || len(units()) != len(before) || funds() != 10 {
		t.Errorf("a refused purchase: %d, %v, %d SP (was %d)", got, units(), funds(), left)
	}
	if len(fake.reads) != 1 {
		t.Errorf("no change reads MekBay: %v", fake.reads)
	}
}

// A paid hangar edit answers with the warchest its charge left, and a refusal with the warchest there is.
func TestPaidHangarEditRepliesWithTheWarchestCharged(t *testing.T) {
	srv, store, _, cookie := startedWith4500(t)
	h := hangarOf("BMDasher_Prime")
	h.Units[0].AmmoBins, h.Units[0].AmmoUsed = 2, 8 // a rearm costs 20 SP
	setRunHangar(t, store, "wolfpack2026", "refusalwar", h)
	rearm := func() *http.Response {
		return post(t, srv, "/api/hangar/service", url.Values{"campaign": {"refusalwar"}, "kind": {"rearm"}, "unit": {"ua"}}, cookie, srv.URL)
	}

	if _, err := store.DB().Exec(`UPDATE campaign_runs SET warchest = 1000`); err != nil {
		t.Fatal(err)
	}
	res := rearm()
	var got struct{ Cost, Warchest int }
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil || res.StatusCode != http.StatusOK || got.Cost != 20 || got.Warchest != 980 {
		t.Errorf("a rearm with 1,000 SP: %d %+v %v", res.StatusCode, got, err)
	}

	setRunHangar(t, store, "wolfpack2026", "refusalwar", h)
	if _, err := store.DB().Exec(`UPDATE campaign_runs SET warchest = 5`); err != nil {
		t.Fatal(err)
	}
	res = rearm()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusConflict || !strings.Contains(string(b), "that costs 20 SP and you have 5.") {
		t.Errorf("a rearm the warchest can't cover: %d %q", res.StatusCode, b)
	}
	if u, _ := runUnit(t, store, "wolfpack2026", "refusalwar", "ua"); u.AmmoBins != 2 {
		t.Errorf("a refused rearm leaves the unit as it was: %+v", u)
	}
}
