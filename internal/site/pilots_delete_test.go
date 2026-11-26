package site

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/davidzech/Battletech/internal/storage"
)

// Deleting a campaign deletes what it saved in MekBay too, after the request has answered: the site's copy of the hangar, every checkpoint's frozen copy and
// every battle it recorded.
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
	run, _, _ := store.Run(ctx, id, "refusalwar")
	copies, _ := store.CheckpointHangars(ctx, id, "refusalwar")
	if !run.Owned() || run.HangarInstance == "" || len(copies) == 0 {
		t.Fatalf("the campaign should own its hangar and have a checkpoint copy: %+v %v", run, copies)
	}
	if err := store.SaveMekBayOperation(ctx, id, "refusalwar", "scenario_000", storage.MekBayOperation{OpForInstance: "opfor", OperationID: "op-live"}); err != nil {
		t.Fatal(err)
	}

	if r := post(t, srv, "/api/campaign/delete", url.Values{"campaign": {"refusalwar"}}, cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", r.StatusCode)
	}
	want := append([]string{run.HangarInstance}, copies...)
	eventually(t, "the hangar and its frozen copies are deleted in MekBay", func() (bool, any) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		for _, f := range want {
			if !slices.Contains(fake.deletes, f) {
				return false, fake.deletes
			}
		}
		return true, nil
	})
	eventually(t, "the battle is deleted in MekBay", func() (bool, any) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return slices.Contains(fake.battleDeletes, "op-live"), fake.battleDeletes
	})
}
