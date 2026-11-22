package site

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/mekbay"

	"github.com/davidzech/Battletech/internal/catalog"
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
	if _, err := store.db.Exec(`UPDATE campaign_runs SET warchest = 4500`); err != nil {
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
	// A run from before the site kept its own copy of the hangar has to be imported again first.
	_, _ = store.db.Exec(`UPDATE campaign_runs SET source_url = ''`)
	if got := buy("BMMadCat_H"); got != http.StatusConflict || funds() != 4500 {
		t.Errorf("a hangar the site doesn't own: %d, funds %d", got, funds())
	}
	_, _ = store.db.Exec(`UPDATE campaign_runs SET source_url = ?`, goodHangarURL)

	// BV is SP: two units cost the sum of their base BV, and both are added to the hangar in MekBay.
	want := madCat.BV + koshi.BV
	if got := buy("BMMadCat_H", "BMKoshi_E"); got != http.StatusOK || funds() != 4500-want {
		t.Fatalf("buy: %d, funds %d (want %d)", got, funds(), 4500-want)
	}
	if len(fake.purchases) != 1 || strings.Join(fake.purchases[0].Units, ",") != "BMMadCat_H,BMKoshi_E" || fake.purchases[0].OwnerUID != mekbay.MekBayUID("wolfpack2026", mekbay.SlotHangar) || fake.purchases[0].Instance == goodInstance || fake.purchases[0].Instance == "" {
		t.Errorf("MekBay was asked: %+v", fake.purchases)
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
	if got := buy("BMMadCat_H"); got != http.StatusConflict || funds() != madCat.BV-1 || len(fake.purchases) != 1 {
		t.Errorf("too poor: %d, funds %d", got, funds())
	}
	// A MekBay failure refunds the charge.
	if err := store.SetWarchest(ctx, id, "refusalwar", 5000); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.serviceErr = errors.New("MekBay said no")
	fake.mu.Unlock()
	if got := buy("BMMadCat_H"); got != http.StatusBadGateway || funds() != 5000 {
		t.Errorf("MekBay failure: %d, funds %d (should be refunded)", got, funds())
	}
}
