package site

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/mekbay"
)

// A change made to the hangar (here a sale and a purchase) updates the cached copy with the force as MekBay saved it, so the Manage page that reloads next
// embeds the hangar after the change at once, rather than the one from before it until a live read catches up.
func TestHangarChangesRefreshTheCache(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	id := playthroughID("wolfpack2026")
	run, _, _ := store.Run(ctx, id, "refusalwar")
	cached := func() map[string]bool {
		h, _, ok, err := store.CachedHangar(ctx, id, "refusalwar", run.HangarInstance)
		if err != nil || !ok {
			t.Fatalf("cached hangar: %v %v", ok, err)
		}
		out := map[string]bool{}
		for _, u := range h.Units {
			out[u.ID] = true
		}
		return out
	}
	if !cached()["ua"] {
		t.Fatal("the started campaign's cache holds ua")
	}
	if got := post(t, srv, "/api/hangar/sell", url.Values{"campaign": {"refusalwar"}, "unit": {"ua"}}, cookie, srv.URL).StatusCode; got != http.StatusOK {
		t.Fatalf("sell: %d", got)
	}
	if cached()["ua"] {
		t.Error("after the sale the cache no longer holds the unit sold")
	}
	if got := post(t, srv, "/api/hangar/purchase", url.Values{"campaign": {"refusalwar"}, "unit": {"BMGrizzly"}}, cookie, srv.URL).StatusCode; got != http.StatusOK {
		t.Fatalf("buy: %d", got)
	}
	if c := cached(); !c["bought-0"] || c["ua"] {
		t.Errorf("after the purchase the cache holds the unit bought: %v", c)
	}
	// A change MekBay refuses leaves the cache as it was.
	fake.serviceErr = context.DeadlineExceeded
	post(t, srv, "/api/hangar/purchase", url.Values{"campaign": {"refusalwar"}, "unit": {"BMGrizzly"}}, cookie, srv.URL)
	if c := cached(); !c["bought-0"] {
		t.Errorf("a refused change keeps the cache: %v", c)
	}
}

// postDisconnecting posts the form straight to the server's handler with a request context that the returned cancel ends, as the server does when the browser
// goes away. It returns a function that makes the request and answers its status.
func postDisconnecting(srv *httptest.Server, path string, form url.Values, cookie *http.Cookie) (do func() int, cancel context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("POST", srv.URL+path, strings.NewReader(form.Encode())).WithContext(ctx)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", srv.URL)
	req.AddCookie(cookie)
	return func() int {
		rec := httptest.NewRecorder()
		srv.Config.Handler.ServeHTTP(rec, req)
		return rec.Code
	}, cancel
}

// A player who closes the tab while MekBay is being called still gets the SP back when the change fails, and is still paid for a unit that was sold: the
// refund and the payout don't run on the request's context, which is cancelled by then.
func TestDisconnectKeepsRefundAndPayout(t *testing.T) {
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
	h := hangarOf("BMMadCat_H", "BMUller_J")
	h.Units[1].Locations = map[string]mekbay.LocationLoss{"LA": {Armor: 3, Internal: 1}}
	fake.setHangar(h, nil)

	// A repair: the tab closes during the MekBay call, which fails with the request; the charge is given back.
	do, cancel := postDisconnecting(srv, "/api/hangar/service", url.Values{"campaign": {"refusalwar"}, "kind": {"repair"}, "unit": {"ub"}, "level": {"armor"}}, cookie)
	fake.onEdit, fake.serviceErr = cancel, context.Canceled
	if got := do(); got != http.StatusBadGateway || len(fake.services) != 1 {
		t.Fatalf("repair cut off: %d, %d service calls", got, len(fake.services))
	}
	if funds() != 4500 {
		t.Errorf("after a repair cut off by a disconnect the warchest holds %d, want the 4500 it had", funds())
	}

	// A sale: the tab closes during the MekBay call, which goes through; the unit is paid for and the sale is in the history.
	do, cancel = postDisconnecting(srv, "/api/hangar/sell", url.Values{"campaign": {"refusalwar"}, "unit": {"ua"}}, cookie)
	fake.onEdit, fake.serviceErr = cancel, nil
	if got := do(); got != http.StatusOK || len(fake.sales) != 1 {
		t.Fatalf("sale cut off: %d, %d sales", got, len(fake.sales))
	}
	madCat, _ := catalog.ByName("BMMadCat_H")
	if funds() != 4500+madCat.BV/2 {
		t.Errorf("after a sale cut off by a disconnect the warchest holds %d, want %d", funds(), 4500+madCat.BV/2)
	}
	hist, err := store.History(ctx, id, "refusalwar")
	if err != nil {
		t.Fatal(err)
	}
	sold := false
	for _, e := range hist {
		sold = sold || strings.HasPrefix(e.Text, "Sold ")
	}
	if !sold {
		t.Errorf("the sale is missing from the history: %+v", hist)
	}
	if h, _, ok, err := store.CachedHangar(ctx, id, "refusalwar", fake.sales[0].Instance); err != nil || !ok || len(h.Units) != 1 {
		t.Errorf("the cache after the sale: %+v %v %v", h.Units, ok, err)
	}
}

// readHookMekBay is a fakeMekBay that runs onRead whenever the hangar is read: between the run an endpoint loads at its start and the charge or payment it
// makes after reading the hangar, as another request (another tab) would.
type readHookMekBay struct {
	*fakeMekBay
	onRead func()
}

func (f *readHookMekBay) ReadHangar(ctx context.Context, instance string) (mekbay.HangarForce, error) {
	if f.onRead != nil {
		f.onRead()
	}
	return f.fakeMekBay.ReadHangar(ctx, instance)
}

// startedWithHookedMekBay starts Refusal War for a seed on a test server whose MekBay runs onRead on every hangar read, with 4,500 SP in the warchest.
func startedWithHookedMekBay(t *testing.T) (*httptest.Server, *readHookMekBay, *http.Cookie, func(n int) func()) {
	t.Helper()
	fake := &readHookMekBay{fakeMekBay: &fakeMekBay{hangar: startingHangar()}}
	srv, store := newMekBayTestServer(t, fake)
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	if _, err := store.DB().Exec(`UPDATE campaign_runs SET warchest = 4500`); err != nil {
		t.Fatal(err)
	}
	// setTo makes an onRead that sets the warchest to n, as another request spending or earning would.
	setTo := func(n int) func() {
		return func() {
			if _, err := store.DB().Exec(`UPDATE campaign_runs SET warchest = ?`, n); err != nil {
				t.Error(err)
			}
		}
	}
	return srv, fake, cookie, setTo
}

// A paid hangar edit answers with the warchest its charge left, and a refusal with the warchest there is, even when another request changed it after this
// one loaded the run.
func TestPaidHangarEditRepliesWithTheWarchestCharged(t *testing.T) {
	srv, fake, cookie, setTo := startedWithHookedMekBay(t)
	h := hangarOf("BMDasher_Prime")
	h.Units[0].AmmoBins, h.Units[0].AmmoUsed = 2, 8 // a rearm costs 20 SP
	fake.setHangar(h, nil)
	rearm := func() *http.Response {
		return post(t, srv, "/api/hangar/service", url.Values{"campaign": {"refusalwar"}, "kind": {"rearm"}, "unit": {"ua"}}, cookie, srv.URL)
	}

	fake.onRead = setTo(1000)
	res := rearm()
	var got struct{ Cost, Warchest int }
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil || res.StatusCode != http.StatusOK || got.Cost != 20 || got.Warchest != 980 {
		t.Errorf("a rearm after another request left 1,000 SP: %d %+v %v", res.StatusCode, got, err)
	}

	fake.onRead = setTo(5)
	res = rearm()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusConflict || !strings.Contains(string(b), "you have 5.") {
		t.Errorf("a rearm the warchest another request left can't cover: %d %q", res.StatusCode, b)
	}
}
