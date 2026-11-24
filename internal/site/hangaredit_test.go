package site

import (
	"context"
	"net/http"
	"net/url"
	"testing"
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
