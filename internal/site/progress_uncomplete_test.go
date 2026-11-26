package site

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

// A database error while checking whether a mission's outcome was committed refuses the un-complete: skipping the check would let the outcome be committed
// a second time, for double rewards.
func TestUncompleteRefusedWhenTheOutcomeCannotBeRead(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	id := playthroughID("wolfpack2026")
	// Scenario 000 has an outcome; its completion is recorded with one.
	if _, err := store.DB().Exec(`INSERT INTO scenario_completions (playthrough_id, campaign, scenario_id, outcome) VALUES (?, 'refusalwar', 'scenario_000', '{}')`, id); err != nil {
		t.Fatal(err)
	}
	uncomplete := func() int {
		return post(t, srv, "/api/campaign/scenario-complete", url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_000"}, "done": {"0"}}, cookie, srv.URL).StatusCode
	}
	if got := uncomplete(); got != http.StatusConflict {
		t.Fatalf("un-completing a committed outcome: %d", got)
	}
	// The read of the outcome now fails (its column is gone), while the completion itself could still be deleted.
	if _, err := store.DB().Exec(`ALTER TABLE scenario_completions RENAME COLUMN outcome TO outcome_gone`); err != nil {
		t.Fatal(err)
	}
	if got := uncomplete(); got != http.StatusInternalServerError {
		t.Errorf("un-completing when the outcome can't be read: %d", got)
	}
	if done, err := store.Completions(ctx, id, "refusalwar"); err != nil || !done["scenario_000"] {
		t.Errorf("the mission stays completed: %v %v", done, err)
	}
}
