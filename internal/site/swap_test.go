package site

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/davidzech/Battletech/internal/catalog"
)

func TestSwapPriceAndOptions(t *testing.T) {
	prime, ok := catalog.ByName("BMDasher_Prime")
	if !ok || prime.Omni == 0 {
		t.Fatal("the Fire Moth Prime should be an omnimech in the catalog")
	}
	opts := swapOptions(prime)
	if len(opts) < 2 {
		t.Fatalf("a Fire Moth has several variants: %+v", opts)
	}
	var up, down *swapOption
	for i, o := range opts {
		v, _ := catalog.ByName(o.Unit)
		if v.Chassis != prime.Chassis || o.Unit == prime.Name || v.Omni == 0 {
			t.Errorf("an option must be another omni variant of the same chassis: %+v", o)
		}
		switch {
		case o.BV > prime.BV && up == nil:
			up = &opts[i]
		case o.BV < prime.BV && down == nil:
			down = &opts[i]
		}
	}
	if up == nil || down == nil {
		t.Fatalf("expected a dearer and a cheaper variant: %+v", opts)
	}
	// Half the tonnage (20t: 10 SP) plus half the BV gained; a cheaper variant costs only the half tonnage.
	if want := 10 + (up.BV-prime.BV)/2; up.Cost != want {
		t.Errorf("up cost = %d, want %d", up.Cost, want)
	}
	if down.Cost != 10 {
		t.Errorf("down cost = %d, want 10 and no refund", down.Cost)
	}
	// An odd tonnage rounds the half up.
	if got := swapPrice(catalog.Unit{Tons: 55, BV: 1000}, catalog.Unit{BV: 1000}); got != 28 {
		t.Errorf("55t swap = %d, want 28", got)
	}
	// Being an omnimech is an advantage over selling and buying: the swap is cheaper than the new BV less half the old one.
	if up.Cost >= up.BV-prime.BV/2 {
		t.Errorf("swapping (%d) should beat selling and rebuying (%d)", up.Cost, up.BV-prime.BV/2)
	}
	// And swapping up then selling a clean unit never makes money.
	if sellValue(up.BV, true)-sellValue(prime.BV, true)-up.Cost >= 0 {
		t.Error("swap up and sell must lose the half tonnage at least")
	}
	// A unit that is not an omnimech has no variants to swap to.
	grizzly, _ := catalog.ByName("BMGrizzly")
	if grizzly.Omni != 0 || swapOptions(grizzly) != nil {
		t.Errorf("the Grizzly is not an omnimech: %+v", swapOptions(grizzly))
	}
}

func TestSwapUnitState(t *testing.T) {
	var src map[string]any
	if err := json.Unmarshal([]byte(wornForce), &src); err != nil {
		t.Fatal(err)
	}
	if err := swapUnitState(src, "u1", "BMMadCat_A", time.Now()); err != nil {
		t.Fatal(err)
	}
	um := src["groups"].([]any)[0].(map[string]any)["units"].([]any)[0].(map[string]any)
	st := um["state"].(map[string]any)
	if um["unit"] != "BMMadCat_A" || len(st["locations"].(map[string]any)) != 0 || len(st["crits"].([]any)) != 0 || st["destroyed"] != false {
		t.Errorf("the unit should be the new variant, fresh: %+v", um)
	}
	if swapUnitState(src, "nope", "x", time.Now()) == nil {
		t.Error("a unit that isn't in the force can't be swapped")
	}
}

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
	if _, err := store.db.Exec(`UPDATE campaign_runs SET warchest = 4500`); err != nil {
		t.Fatal(err)
	}
	id := playthroughID("wolfpack2026")
	prime, _ := catalog.ByName("BMDasher_Prime")
	var up swapOption
	for _, o := range swapOptions(prime) {
		if o.BV > prime.BV {
			up = o
		}
	}
	// ua: a clean Fire Moth Prime, ub: a Grizzly (not an omnimech), uc: a Fire Moth with spent ammo.
	h := hangarOf("BMDasher_Prime", "BMGrizzly", "BMDasher_Prime")
	h.Units[2].Damaged, h.Units[2].AmmoBins, h.Units[2].AmmoUsed = true, 1, 3
	fake.setHangar(h, nil)
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
	if len(fake.swaps) != 0 {
		t.Fatalf("refused swaps reached MekBay: %+v", fake.swaps)
	}
	if got := swap("u-ghost", up.Unit); got != http.StatusNotFound {
		t.Errorf("unknown unit: %d", got)
	}
	// A good swap charges the price and tells MekBay.
	if got := swap("ua", up.Unit); got != http.StatusOK || funds() != 4500-up.Cost {
		t.Fatalf("swap: %d, funds %d (want %d)", got, funds(), 4500-up.Cost)
	}
	if len(fake.swaps) != 1 || fake.swaps[0].UnitID != "ua" || fake.swaps[0].To != up.Unit || fake.swaps[0].OwnerUID != MekBayUID("wolfpack2026", SlotHangar) {
		t.Errorf("MekBay was asked: %+v", fake.swaps)
	}
	// A failure in MekBay refunds, and too little in the warchest refuses.
	before := funds()
	fake.serviceErr = context.DeadlineExceeded
	if got := swap("ua", up.Unit); got != http.StatusBadGateway || funds() != before {
		t.Errorf("MekBay failure: %d, funds %d (want %d)", got, funds(), before)
	}
	fake.serviceErr = nil
	if err := store.SetWarchest(ctx, id, "refusalwar", up.Cost-1); err != nil {
		t.Fatal(err)
	}
	if got := swap("ua", up.Unit); got != http.StatusConflict || funds() != up.Cost-1 {
		t.Errorf("too poor: %d, funds %d", got, funds())
	}
	// The hangar view carries the options for the page, only for omnimechs.
	views := hangarViews(h)
	if len(views[0].SwapOptions) < 2 || len(views[1].SwapOptions) != 0 {
		t.Errorf("view options: %+v / %+v", views[0].SwapOptions, views[1].SwapOptions)
	}
}
