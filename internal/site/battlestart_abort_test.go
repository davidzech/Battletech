package site

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/storage"
)

// A rollback asked for on abort never silently becomes "keep": when the checkpoints can't be read the abort fails, and the battle stays.
func TestAbortWithRollbackFailsWhenCheckpointsCannotBeRead(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	const seed = "wolfpack2026"
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {seed}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	id := playthroughID(seed)
	if err := store.SaveMekBayOperation(ctx, id, "refusalwar", "scenario_000", storage.MekBayOperation{OpForInstance: "opfor", OperationID: "op-1"}); err != nil {
		t.Fatal(err)
	}
	// The checkpoints can't be read: a column the listing needs is gone.
	if _, err := store.DB().Exec(`ALTER TABLE campaign_checkpoints RENAME COLUMN label TO label_gone`); err != nil {
		t.Fatal(err)
	}
	abort := func(mode string) int {
		return post(t, srv, "/api/battle/abort", url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_000"}, "mode": {mode}}, cookie, srv.URL).StatusCode
	}
	if got := abort("rollback"); got != http.StatusInternalServerError {
		t.Errorf("abort with rollback when the checkpoints can't be read: %d", got)
	}
	if _, had, _ := store.MekBayOperation(ctx, id, "refusalwar", "scenario_000"); !had {
		t.Error("the failed abort leaves the battle")
	}
	if h, _ := store.History(ctx, id, "refusalwar"); len(h) > 0 && strings.HasPrefix(h[len(h)-1].Text, "Aborted ") {
		t.Errorf("the failed abort is not in the history: %q", h[len(h)-1].Text)
	}
	// Keeping the changes needs no checkpoint, so it still works.
	if got := abort("keep"); got != http.StatusNoContent {
		t.Errorf("abort keeping the changes: %d", got)
	}
}
