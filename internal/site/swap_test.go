package site

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/hangar"
)

func TestSwapUnitEndpoint(t *testing.T) {
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
	prime, _ := catalog.ByName("BMDasher_Prime")
	var up hangar.SwapOption
	for _, o := range hangar.SwapOptions(prime) {
		if o.BV > prime.BV {
			up = o
		}
	}
	// ua: a clean Fire Moth Prime, ub: a Grizzly (not an omnimech), uc: a Fire Moth with spent ammo.
	h := hangarOf("BMDasher_Prime", "BMGrizzly", "BMDasher_Prime")
	h.Units[2].Damaged, h.Units[2].AmmoBins, h.Units[2].AmmoUsed = true, 1, 3
	setRunHangar(t, store, "wolfpack2026", "refusalwar", h)
	design := func(id string) string { u, _ := runUnit(t, store, "wolfpack2026", "refusalwar", id); return u.Unit }
	funds := func() int { r, _, _ := store.Run(ctx, id, "refusalwar"); return r.Warchest }
	swap := func(unit, to string) int {
		return post(t, srv, "/api/hangar/swap", url.Values{"campaign": {"refusalwar"}, "unit": {unit}, "to": {to}}, cookie, srv.URL).StatusCode
	}

	if got := post(t, srv, "/api/hangar/swap", url.Values{"campaign": {"refusalwar"}, "unit": {"ua"}, "to": {up.Unit}}, cookie, "https://evil.example").StatusCode; got != http.StatusForbidden {
		t.Errorf("cross-origin: %d", got)
	}
	for name, c := range map[string][2]string{
		"not an omnimech":        {"ub", up.Unit},
		"damaged or unrearmed":   {"uc", up.Unit},
		"a variant of another":   {"ua", "BMMadCat_H"},
		"a variant that is none": {"ua", "BMNoSuchMech"},
		"the same variant":       {"ua", "BMDasher_Prime"},
	} {
		want := http.StatusBadRequest
		if name == "damaged or unrearmed" {
			want = http.StatusConflict
		}
		if got := swap(c[0], c[1]); got != want || funds() != 4500 {
			t.Errorf("%s: %d (want %d), funds %d", name, got, want, funds())
		}
	}
	if design("ua") != "BMDasher_Prime" || design("uc") != "BMDasher_Prime" {
		t.Fatal("refused swaps change nothing")
	}
	if got := swap("u-ghost", up.Unit); got != http.StatusNotFound {
		t.Errorf("unknown unit: %d", got)
	}
	// A good swap charges the price and reconfigures the unit, which keeps its ID.
	if got := swap("ua", up.Unit); got != http.StatusOK || funds() != 4500-up.Cost {
		t.Fatalf("swap: %d, funds %d (want %d)", got, funds(), 4500-up.Cost)
	}
	if design("ua") != up.Unit {
		t.Errorf("the unit is reconfigured: %s", design("ua"))
	}
	// Too little in the warchest refuses, and changes nothing.
	setRunHangar(t, store, "wolfpack2026", "refusalwar", h)
	if err := store.SetWarchest(ctx, id, "refusalwar", up.Cost-1); err != nil {
		t.Fatal(err)
	}
	if got := swap("ua", up.Unit); got != http.StatusConflict || funds() != up.Cost-1 || design("ua") != "BMDasher_Prime" {
		t.Errorf("too poor: %d, funds %d", got, funds())
	}
	// The hangar view carries the options for the page, only for omnimechs.
	views := hangarViews(h)
	if len(views[0].SwapOptions) < 2 || len(views[1].SwapOptions) != 0 {
		t.Errorf("view options: %+v / %+v", views[0].SwapOptions, views[1].SwapOptions)
	}
}
