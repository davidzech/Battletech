package site

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/mekbay"
)

func TestPurchaseEndpoint(t *testing.T) {
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
	funds := func() int { r, _, _ := store.Run(ctx, id, "refusalwar"); return r.Warchest }
	buy := func(units ...string) int {
		v := url.Values{"campaign": {"refusalwar"}, "unit": units}
		return post(t, srv, "/api/hangar/purchase", v, cookie, srv.URL).StatusCode
	}
	madCat, _ := catalog.ByName("BMMadCat_H")
	koshi, _ := catalog.ByName("BMKoshi_E")

	if got := post(t, srv, "/api/hangar/purchase", url.Values{"campaign": {"refusalwar"}, "unit": {"BMMadCat_H"}}, cookie, "https://evil.example").StatusCode; got != http.StatusForbidden {
		t.Errorf("cross-origin: %d", got)
	}
	// BV is SP: two units cost the sum of their base BV, and both are added to the hangar's Hangar group, undamaged with an unnamed 4/5 crew.
	want := madCat.BV + koshi.BV
	if got := buy("BMMadCat_H", "BMKoshi_E"); got != http.StatusOK || funds() != 4500-want {
		t.Fatalf("buy: %d, funds %d (want %d)", got, funds(), 4500-want)
	}
	h := runHangar(t, store, "wolfpack2026", "refusalwar")
	var bought []string
	for _, u := range h.Units[5:] {
		bought = append(bought, u.Unit)
		if u.Group != mekbay.HangarGroup || u.Pilot != "" || u.Gunnery != 4 || u.Piloting != 5 || u.Damaged || u.ID == "" {
			t.Errorf("a unit bought: %+v", u)
		}
	}
	if len(h.Units) != 7 || strings.Join(bought, ",") != "BMMadCat_H,BMKoshi_E" {
		t.Errorf("the hangar after the purchase: %+v", h.Units)
	}
	// Refused: nothing chosen, an unknown unit, and too little in the warchest. Nothing is charged.
	before := funds()
	for name, units := range map[string][]string{"nothing": nil, "unknown unit": {"BMNoSuchMech"}} {
		if got := buy(units...); got != http.StatusBadRequest || funds() != before {
			t.Errorf("%s: %d, funds %d", name, got, funds())
		}
	}
	if err := store.SetWarchest(ctx, id, "refusalwar", madCat.BV-1); err != nil {
		t.Fatal(err)
	}
	if got := buy("BMMadCat_H"); got != http.StatusConflict || funds() != madCat.BV-1 || len(runHangar(t, store, "wolfpack2026", "refusalwar").Units) != 7 {
		t.Errorf("too poor: %d, funds %d", got, funds())
	}
}
