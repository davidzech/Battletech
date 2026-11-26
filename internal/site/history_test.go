package site

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/pilot"
	"github.com/davidzech/Battletech/internal/storage"
)

// TestCampaignHistory walks a campaign through a few changes and checks each lands in the history in the players' terms, with what it did to the warchest, that
// a rollback takes the history back to its checkpoint, and that the campaign page shows it in its History dialog.
func TestCampaignHistory(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()} // ua Koshi, ub Uller, uc Dasher, ud Grizzly
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	const seed = "wolfpack2026"
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {seed}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	id := playthroughID(seed)
	form := func(kv ...string) url.Values {
		v := url.Values{"campaign": {"refusalwar"}}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Add(kv[i], kv[i+1])
		}
		return v
	}
	history := func() []storage.HistoryEntry {
		h, err := store.History(ctx, id, "refusalwar")
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	find := func(prefix string) storage.HistoryEntry {
		for _, e := range history() {
			if strings.HasPrefix(e.Text, prefix) {
				return e
			}
		}
		t.Fatalf("no history entry starting %q in %+v", prefix, history())
		return storage.HistoryEntry{}
	}
	pilots, _ := store.Pilots(ctx, id, "refusalwar")
	alrek := ""
	for _, p := range pilots {
		if p.Name == "Alrek" {
			alrek = p.ID
		}
	}

	if e := find("Started the campaign as"); e.Kind != storage.HistoryCampaign || e.SP != 5000 || !strings.Contains(e.Text, "Alrek and Katarina") {
		t.Errorf("the start: %+v", e)
	}
	post(t, srv, "/api/pilots/allocate", form("pilot", alrek, "earned", "90"), cookie, srv.URL)
	if e := find("Alrek earned 90 SP"); e.Kind != storage.HistoryPilot || e.SP != 0 {
		t.Errorf("earned SP: %+v", e)
	}
	// A mission completed by hand is a checkpoint; its line is written before the checkpoint, so rolling back to it keeps the line.
	if _, err := store.SetScenarioCompleted(ctx, id, "refusalwar", "scenario_000", true); err != nil {
		t.Fatal(err)
	}
	if got := post(t, srv, "/api/campaign/scenario-complete", form("scenario", "scenario_001", "done", "1"), cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("complete 001: %d", got)
	}
	find("Completed Scenario 001: Pyre Light.")
	if got := post(t, srv, "/api/hangar/sell", form("unit", "ua"), cookie, srv.URL).StatusCode; got != http.StatusOK {
		t.Fatalf("sell: %d", got)
	}
	sold := find("Sold ")
	if sold.Kind != storage.HistoryHangar || sold.SP <= 0 {
		t.Errorf("the sale pays the warchest: %+v", sold)
	}
	if got := post(t, srv, "/api/warchest", form("funds", "1234"), cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("set the warchest: %d", got)
	}
	if e := find("The warchest was set to 1,234 SP"); e.Kind != storage.HistoryCorrections || e.SP >= 0 {
		t.Errorf("setting the funds: %+v", e)
	}

	// Rolling back to the end of Scenario 001 takes the history back with the campaign: the sale and the funds never happened, everything before stays, and one
	// line says the rollback happened.
	cps, _ := store.Checkpoints(ctx, id, "refusalwar")
	if got := post(t, srv, "/api/campaign/rollback", form("checkpoint", strconv.Itoa(cps[0].Seq)), cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("rollback: %d", got)
	}
	var texts []string
	for _, e := range history() {
		texts = append(texts, e.Text)
	}
	if len(texts) != 4 || !strings.HasPrefix(texts[0], "Started the campaign") || !strings.HasPrefix(texts[1], "Alrek earned 90 SP") || texts[2] != "Completed Scenario 001: Pyre Light." || texts[3] != "Rolled back to \u201cCompleted Scenario 001: Pyre Light\u201d." {
		t.Errorf("after the rollback the history ends at the checkpoint, then the rollback's own line: %q", texts)
	}
	if h := history(); h[len(h)-1].Kind != storage.HistoryCorrections {
		t.Errorf("a rollback is a correction: %+v", h[len(h)-1])
	}
	// Logging goes on from there.
	post(t, srv, "/api/campaign/force-name", form("name", "Iron Wolves"), cookie, srv.URL)
	if h := history(); h[len(h)-1].Text != "The force was renamed Iron Wolves." || h[len(h)-1].Seq != 5 {
		t.Errorf("the next line follows the checkpoint: %+v", h[len(h)-1])
	}

	// A portrait change is logged but left out of the History dialog.
	taken := map[string]bool{}
	ps, _ := store.Pilots(ctx, id, "refusalwar")
	for _, p := range ps {
		taken[p.Portrait] = true
	}
	if r := post(t, srv, "/api/pilots/portrait", form("pilot", alrek, "portrait", pilot.RandomPortrait(taken)), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("portrait: %d", r.StatusCode)
	}
	if h := history(); !strings.HasSuffix(h[len(h)-1].Text, portraitHistorySuffix) {
		t.Errorf("the portrait change is logged: %+v", h[len(h)-1])
	}
	if strings.Contains(body(t, srv, "/refusalwar/", cookie), "changed portrait") {
		t.Error("the History dialog leaves portrait changes out")
	}

	// The campaign page has the History button and dialog, newest first.
	page := body(t, srv, "/refusalwar/", cookie)
	if !strings.Contains(page, `id="history-open">History</button>`) || !strings.Contains(page, `id="history-dialog"`) || !strings.Contains(page, "/static/campaignhistory.js") {
		t.Fatal("a started campaign's page has a History button and dialog")
	}
	if strings.Index(page, "The force was renamed Iron Wolves.") > strings.Index(page, "Started the campaign as") {
		t.Error("the history is newest first")
	}
	if strings.Contains(page, "Sold ") || !strings.Contains(page, "Rolled back to") {
		t.Error("nothing the rollback took back is shown, but the rollback is")
	}
	// The lines a checkpoint was taken right after offer to roll back to it; the others don't.
	if !strings.Contains(page, `data-label="Campaign started"`) || !strings.Contains(page, `data-label="Completed Scenario 001: Pyre Light"`) || strings.Count(page, `class="button button-outline history-rollback"`) != 2 {
		t.Errorf("the start and the completed mission can be rolled back to, nothing else: %d buttons", strings.Count(page, "history-rollback\""))
	}
	if i := strings.Index(page, "The force was renamed Iron Wolves."); i < 0 || strings.Contains(page[i:i+300], "history-rollback") {
		t.Error("a line with no checkpoint after it has no Roll back button")
	}
	if !strings.Contains(page, `<span class="history-sp gain">&#43;5,000 SP</span>`) {
		t.Error("the start's warchest shows as a gain")
	}
	if !strings.Contains(page, `data-kind="mission" aria-pressed="false">Mission</button>`) || !strings.Contains(page, `data-kind="hangar" aria-pressed="false" disabled title="Nothing of this kind yet">Hangar</button>`) {
		t.Error("every kind has a filter, disabled for a kind the history doesn't hold")
	}

	// With one kind of entry, the other filters are disabled.
	if _, err := store.SetScenarioCompleted(ctx, id, "refusalwar", "scenario_001", false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RestoreCheckpoint(ctx, id, "refusalwar", cps[len(cps)-1].Seq); err != nil { // back to the start: only its line is left
		t.Fatal(err)
	}
	if h := history(); len(h) != 1 {
		t.Fatalf("back at the start: %+v", h)
	}
	if page := body(t, srv, "/refusalwar/", cookie); !strings.Contains(page, `data-kind="mission" aria-pressed="false" disabled`) || !strings.Contains(page, `data-kind="campaign" aria-pressed="false">Campaign</button>`) {
		t.Error("a history of one kind keeps every filter, only that kind's enabled")
	}

	// Deleting the campaign deletes its history.
	if _, err := store.DeleteRun(ctx, id, "refusalwar"); err != nil {
		t.Fatal(err)
	}
	if h := history(); len(h) != 0 {
		t.Errorf("a deleted campaign keeps no history: %v", h)
	}
}
