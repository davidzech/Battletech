package site

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/davidzech/Battletech/internal/storage"
)

// Deleting a campaign deletes its hangar and checkpoints with its rows, and every battle it recorded in MekBay after the request has answered.
func TestDeleteCampaignCleansUpMekBay(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	id := playthroughID("wolfpack2026")
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	if cps, _ := store.Checkpoints(ctx, id, "refusalwar"); len(cps) == 0 {
		t.Fatal("the campaign should have a checkpoint")
	}
	if err := store.SaveMekBayOperation(ctx, id, "refusalwar", "scenario_000", storage.MekBayOperation{OpForInstance: "opfor", OperationID: "op-live"}); err != nil {
		t.Fatal(err)
	}

	if r := post(t, srv, "/api/campaign/delete", url.Values{"campaign": {"refusalwar"}}, cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", r.StatusCode)
	}
	if _, err := store.Hangar(ctx, id, "refusalwar"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("the hangar is gone with the campaign: %v", err)
	}
	if cps, _ := store.Checkpoints(ctx, id, "refusalwar"); len(cps) != 0 {
		t.Errorf("and its checkpoints: %+v", cps)
	}
	eventually(t, "the battle is deleted in MekBay", func() (bool, any) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return slices.Contains(fake.battleDeletes, "op-live"), fake.battleDeletes
	})
}
