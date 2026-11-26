package site

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/davidzech/Battletech/internal/storage"
)

// A mission completed by hand ends its battle, which nothing refers to any more: it is deleted in MekBay, in the background.
func TestCompletingByHandDeletesTheBattle(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	id := playthroughID("wolfpack2026")
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	if got := post(t, srv, "/api/campaign/scenario-outcome/commit", url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_000"}, "inputs": {`{"pilots":[],"mission":{}}`}}, cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("complete 000: %d", got)
	}
	if err := store.SaveMekBayOperation(ctx, id, "refusalwar", "scenario_001", storage.MekBayOperation{OpForInstance: "opfor", OperationID: "op-001"}); err != nil {
		t.Fatal(err)
	}
	if got := post(t, srv, "/api/campaign/scenario-complete", url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_001"}, "done": {"1"}}, cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("complete 001 by hand: %d", got)
	}
	eventually(t, "the completed mission's battle is deleted in MekBay", func() (bool, any) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return slices.Contains(fake.battleDeletes, "op-001"), fake.battleDeletes
	})
}
