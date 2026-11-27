package site

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/mekbay"
)

// A sale answers with the warchest its payment made.
func TestSellRepliesWithTheWarchestPaid(t *testing.T) {
	srv, store, _, cookie := startedWith4500(t)
	setRunHangar(t, store, "wolfpack2026", "refusalwar", hangarOf("BMMadCat_H"))
	madCat, _ := catalog.ByName("BMMadCat_H")
	if _, err := store.DB().Exec(`UPDATE campaign_runs SET warchest = 1000`); err != nil {
		t.Fatal(err)
	}
	res := post(t, srv, "/api/hangar/sell", url.Values{"campaign": {"refusalwar"}, "unit": {"ua"}}, cookie, srv.URL)
	var got struct{ Value, Warchest int }
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil || res.StatusCode != http.StatusOK || got.Value != madCat.BV/2 || got.Warchest != 1000+madCat.BV/2 {
		t.Errorf("a sale with 1,000 SP: %d %+v %v (want %d)", res.StatusCode, got, err, 1000+madCat.BV/2)
	}
}

func TestSellUnitEndpoint(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	// These tests are about prices, not the campaign's starting funds: give them a round balance.
	if _, err := store.DB().Exec(`UPDATE campaign_runs SET warchest = 4500`); err != nil {
		t.Fatal(err)
	}
	id := playthroughID("wolfpack2026")
	// ua is untouched, ub has spent ammo, uc is truly destroyed (center torso gone), ud is unknown to the catalog.
	h := hangarOf("BMMadCat_H", "BMDasher_Prime", "BMKoshi_E")
	h.Units[1].AmmoBins, h.Units[1].AmmoUsed = 1, 4
	h.Units[1].Damaged = true
	h.Units[2].Destroyed, h.Units[2].Locations = true, map[string]mekbay.LocationLoss{"CT": {Internal: 8}}
	h.Units[2].Damaged = true
	setRunHangar(t, store, "wolfpack2026", "refusalwar", h)
	funds := func() int { r, _, _ := store.Run(ctx, id, "refusalwar"); return r.Warchest }
	sell := func(unit string) int {
		return post(t, srv, "/api/hangar/sell", url.Values{"campaign": {"refusalwar"}, "unit": {unit}}, cookie, srv.URL).StatusCode
	}
	madCat, _ := catalog.ByName("BMMadCat_H")
	dasher, _ := catalog.ByName("BMDasher_Prime")
	koshi, _ := catalog.ByName("BMKoshi_E")

	if got := post(t, srv, "/api/hangar/sell", url.Values{"campaign": {"refusalwar"}, "unit": {"ua"}}, cookie, "https://evil.example").StatusCode; got != http.StatusForbidden {
		t.Errorf("cross-origin: %d", got)
	}
	// A fully repaired and rearmed unit sells for half its BV; the unit leaves the hangar.
	if got := sell("ua"); got != http.StatusOK || funds() != 4500+madCat.BV/2 {
		t.Fatalf("sell pristine: %d, funds %d (want %d)", got, funds(), 4500+madCat.BV/2)
	}
	if _, still := runUnit(t, store, "wolfpack2026", "refusalwar", "ua"); still || len(runHangar(t, store, "wolfpack2026", "refusalwar").Units) != 2 {
		t.Errorf("the unit sold is gone from the hangar: %+v", runHangar(t, store, "wolfpack2026", "refusalwar").Units)
	}
	// Worn units sell for a quarter: spent ammunition, and a truly destroyed mech.
	before := funds()
	if got := sell("ub"); got != http.StatusOK || funds() != before+dasher.BV/4 {
		t.Errorf("sell rearm-needing: %d, funds %d (want %d)", got, funds(), before+dasher.BV/4)
	}
	before = funds()
	if got := sell("uc"); got != http.StatusOK || funds() != before+koshi.BV/4 {
		t.Errorf("sell truly destroyed: %d, funds %d (want %d)", got, funds(), before+koshi.BV/4)
	}
	// Unknown units are refused, and so is a unit already sold (a second click): nothing more is paid.
	before = funds()
	if got := sell("u-ghost"); got != http.StatusNotFound || funds() != before {
		t.Errorf("unknown unit: %d, funds %d", got, funds())
	}
	if got := sell("ua"); got != http.StatusNotFound || funds() != before {
		t.Errorf("selling again: %d, funds %d", got, funds())
	}
	// A truly destroyed mech can't be rearmed either: selling is all that is left.
	h = hangarOf("BMKoshi_E")
	h.Units[0].Destroyed, h.Units[0].Damaged, h.Units[0].Locations = true, true, map[string]mekbay.LocationLoss{"CT": {Internal: 8}}
	h.Units[0].AmmoBins, h.Units[0].AmmoUsed = 1, 3
	setRunHangar(t, store, "wolfpack2026", "refusalwar", h)
	if got := post(t, srv, "/api/hangar/service", url.Values{"campaign": {"refusalwar"}, "kind": {"rearm"}, "unit": {"ua"}}, cookie, srv.URL).StatusCode; got != http.StatusConflict {
		t.Errorf("rearming a truly destroyed unit: %d", got)
	}
	// The hangar view carries the price for the page.
	if p := body(t, srv, "/refusalwar/manage/", cookie); !strings.Contains(p, "sellValue") {
		t.Error("the Manage page's hangar should carry each unit's sell value")
	}
}
