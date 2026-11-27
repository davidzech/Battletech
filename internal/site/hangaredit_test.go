package site

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/mekbay"
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
	if len(u) != 5 || grizzlies != 1 {
		t.Errorf("after the purchase the hangar holds the unit bought: %v", u)
	}
	if p := body(t, srv, "/refusalwar/manage/", cookie); !strings.Contains(p, "Grizzly") {
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

// A campaign whose hangar couldn't be imported from MekBay (none stored) says so, the same way, wherever its hangar is needed: the hangar's JSON for the pages,
// every change to it, the Deploy popup and Start battle.
func TestCampaignWithoutAHangar(t *testing.T) {
	srv, store, _, cookie := startedWith4500(t)
	if _, err := store.DB().Exec(`UPDATE campaign_runs SET hangar = ''`); err != nil {
		t.Fatal(err)
	}
	check := func(what string, res *http.Response) {
		t.Helper()
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusConflict || strings.TrimSpace(string(b)) != noHangarMessage {
			t.Errorf("%s: %d %s", what, res.StatusCode, b)
		}
	}
	form := func(kv ...string) url.Values {
		v := url.Values{"campaign": {"refusalwar"}}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Add(kv[i], kv[i+1])
		}
		return v
	}
	check("force JSON", get(t, srv, "/api/campaign/force?campaign=refusalwar", cookie))
	check("deploy hangar", get(t, srv, "/api/mekbay/hangar?campaign=refusalwar&scenario=scenario_001", cookie))
	check("repair", post(t, srv, "/api/hangar/service", form("unit", "ua", "kind", "repair", "level", "armor"), cookie, srv.URL))
	check("sell", post(t, srv, "/api/hangar/sell", form("unit", "ua"), cookie, srv.URL))
	check("purchase", post(t, srv, "/api/hangar/purchase", form("unit", "BMGrizzly"), cookie, srv.URL))
	check("swap", post(t, srv, "/api/hangar/swap", form("unit", "ua", "to", "BMKoshi_A"), cookie, srv.URL))
	check("start battle", post(t, srv, "/api/battle/start", form("scenario", "scenario_001", "players", "1", "p1_units", "ua"), cookie, srv.URL))
	if r, _, _ := store.Run(t.Context(), playthroughID("wolfpack2026"), "refusalwar"); r.Warchest != 4500 {
		t.Errorf("nothing is charged: %d", r.Warchest)
	}
	// The pages still render (they fetch the hangar, and show the message).
	for _, path := range []string{"/refusalwar/", "/refusalwar/manage/", "/refusalwar/scenario_001/"} {
		if r := get(t, srv, path, cookie); r.StatusCode != http.StatusOK {
			t.Errorf("%s: %d", path, r.StatusCode)
		}
	}
}

// A hangar edit that passed the battle lock is still refused if a battle is started (claimed) before its transaction: the check inside the transaction gives the
// battle lock's own message, and the hangar and the warchest are left as they were.
func TestHangarEditRefusedWhenABattleStartsAfterTheLockCheck(t *testing.T) {
	srv, store, _, cookie := startedWith4500(t)
	s := &Server{lib: content.Library{Root: plainScenario001Root(t)}, store: store}
	id := playthroughID("wolfpack2026")
	pt, _, err := store.Playthrough(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := s.lib.Campaign("refusalwar")
	before := len(runHangar(t, store, "wolfpack2026", "refusalwar").Units)
	// The request has passed runRequest's battle lock; now a start claims the campaign.
	if ok, err := store.ClaimBattle(t.Context(), id, "refusalwar", "scenario_001"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/hangar/sell", nil)
	_, _, ok := s.changeHangar(w, r, pt, c, func(force map[string]any, h mekbay.HangarForce) (int, string, error) {
		mekbay.RemoveUnitFromForce(force, "ua")
		return 100, "Sold a unit.", nil
	})
	sc, _ := c.ScenarioByID("scenario_001")
	if ok || w.Code != http.StatusConflict || strings.TrimSpace(w.Body.String()) != battleLockMessage(c, sc) {
		t.Errorf("an edit after a battle was claimed: ok=%v %d %q", ok, w.Code, w.Body.String())
	}
	if n := len(runHangar(t, store, "wolfpack2026", "refusalwar").Units); n != before {
		t.Errorf("the hangar is unchanged: %d units, was %d", n, before)
	}
	if run, _, _ := store.Run(t.Context(), id, "refusalwar"); run.Warchest != 4500 {
		t.Errorf("nothing is paid: %d", run.Warchest)
	}
	// The same message as the battle lock gives a request that comes later.
	res := post(t, srv, "/api/hangar/sell", url.Values{"campaign": {"refusalwar"}, "unit": {"ua"}}, cookie, srv.URL)
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusConflict || strings.TrimSpace(string(b)) != battleLockMessage(c, sc) {
		t.Errorf("the battle lock: %d %s", res.StatusCode, b)
	}
}
