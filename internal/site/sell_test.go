package site

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/mekbay"

	"github.com/davidzech/Battletech/internal/catalog"
)

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
	fake.setHangar(h, nil)
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
	// A run from before the site kept its own copy can't sell.
	_, _ = store.DB().Exec(`UPDATE campaign_runs SET source_url = ''`)
	if got := sell("ua"); got != http.StatusConflict || funds() != 4500 {
		t.Errorf("a hangar the site doesn't own: %d, funds %d", got, funds())
	}
	_, _ = store.DB().Exec(`UPDATE campaign_runs SET source_url = ?`, goodHangarURL)

	// A fully repaired and rearmed unit sells for half its BV; the unit leaves the hangar in MekBay.
	if got := sell("ua"); got != http.StatusOK || funds() != 4500+madCat.BV/2 {
		t.Fatalf("sell pristine: %d, funds %d (want %d)", got, funds(), 4500+madCat.BV/2)
	}
	if len(fake.sales) != 1 || fake.sales[0].UnitID != "ua" || fake.sales[0].OwnerUID != mekbay.MekBayUID("wolfpack2026", mekbay.SlotHangar) || fake.sales[0].Instance == goodInstance {
		t.Errorf("MekBay was asked: %+v", fake.sales)
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
	// Unknown units are refused, and a failure in MekBay pays nothing.
	before = funds()
	if got := sell("u-ghost"); got != http.StatusNotFound || funds() != before {
		t.Errorf("unknown unit: %d, funds %d", got, funds())
	}
	fake.serviceErr = context.DeadlineExceeded
	fake.setHangar(hangarOf("BMMadCat_H"), nil)
	if got := sell("ua"); got != http.StatusBadGateway || funds() != before {
		t.Errorf("MekBay failure: %d, funds %d", got, funds())
	}
	// A truly destroyed mech can't be rearmed either: selling is all that is left.
	fake.serviceErr = nil
	h = hangarOf("BMKoshi_E")
	h.Units[0].Destroyed, h.Units[0].Damaged, h.Units[0].Locations = true, true, map[string]mekbay.LocationLoss{"CT": {Internal: 8}}
	h.Units[0].AmmoBins, h.Units[0].AmmoUsed = 1, 3
	fake.setHangar(h, nil)
	if got := post(t, srv, "/api/hangar/service", url.Values{"campaign": {"refusalwar"}, "kind": {"rearm"}, "unit": {"ua"}}, cookie, srv.URL).StatusCode; got != http.StatusConflict {
		t.Errorf("rearming a truly destroyed unit: %d", got)
	}
	// The hangar view carries the price for the page.
	if p := body(t, srv, "/refusalwar/manage/", cookie); !strings.Contains(p, "sellValue") {
		t.Error("the Manage page's cached hangar should carry each unit's sell value")
	}
}
