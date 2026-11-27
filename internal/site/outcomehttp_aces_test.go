package site

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/storage"
)

// The whole Aces-style flow on a scratch copy of Refusal War whose Scenario 000 is replaced by an Aces-style mission: four pilots, two active (one of them killed),
// one benched; two objectives met; pilot SP of 80.
func TestAcesMissionEndToEnd(t *testing.T) {
	root := t.TempDir()
	camp := filepath.Join(root, "RefusalWar")
	write := func(path, text string) {
		p := filepath.Join(camp, path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	intro, err := os.ReadFile("../../campaigns/RefusalWar/Intro.md")
	if err != nil {
		t.Fatal(err)
	}
	write("Intro.md", string(intro))
	write("Keywords.md", "---\nkeywords:\n  - {id: depot_gone, name: Depot Gone, scenario: scenario_000, description: The depot was destroyed.}\n  - {id: depot_standing, name: Depot Standing, scenario: scenario_000, description: The depot still stands.}\n  - {id: data_saved, name: Data Saved, scenario: scenario_000, description: The data got out.}\n---\n")
	write("Scenario_000/Setup.md", "---\nid: scenario_000\ntitle: Raid\nplanet: Colmar\nforces: {}\n---\n# Raid\n")
	write("Scenario_000/Outcome.md", `---
id: scenario_000
pilot_sp: 80
objectives:
  - id: depot
    label: Destroy the depot
    sp: 400
    grants: [depot_gone]
    unmet_grants: [depot_standing]
  - id: data
    label: Escape with the data
    sp: 200
    grants: [data_saved]
outcomes:
  - when: not depot and not data
    label: A total failure
    warchest: -100
aftermath:
  - when: depot
    text: The depot burns behind you.
  - when: not depot
    text: The depot still stands.
  - text: The Keshik takes note.
---
# Raid - Outcome

## Aftermath

The war goes on without you.

## Next Steps

Rest, then raid again.
`)
	write("Scenario_001/Setup.md", "---\nid: scenario_001\ntitle: Next\n---\n# Next\n")

	fake := &fakeMekBay{hangar: startingHangar()}
	store, err := storage.OpenSQLite(t.TempDir() + "/aces.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DB().Close() })
	srv := newTestServerAt(t, root, store, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	id := playthroughID("wolfpack2026")
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	for _, n := range []string{"Jarek", "Sybille"} { // four pilots, at 300 SP each for the two recruits
		if got := post(t, srv, "/api/pilots", url.Values{"campaign": {"refusalwar"}, "name": {n}, "gunnery": {"90"}, "edge_tokens": {"60"}}, cookie, srv.URL).StatusCode; got != http.StatusNoContent {
			t.Fatalf("recruit %s: %d", n, got)
		}
	}
	pilots, _ := store.Pilots(ctx, id, "refusalwar")
	pid := map[string]string{}
	total := map[string]int{}
	for _, p := range pilots {
		pid[p.Name], total[p.Name] = p.ID, p.TotalSP
	}
	run0, _, _ := store.Run(ctx, id, "refusalwar")

	page := body(t, srv, "/refusalwar/scenario_000/", cookie)
	if !strings.Contains(page, `id="outcome-open"`) || !strings.Contains(page, "Destroy the depot") || !strings.Contains(page, "400 SP") || !strings.Contains(page, "Escape with the data") {
		t.Fatalf("an Aces-style mission completes by reporting; its objectives reach the dialog: %.500s", page)
	}
	report := func(depot, data bool, entries ...content.OutcomeEntry) url.Values {
		b, _ := json.Marshal(content.OutcomeInputs{Pilots: entries, Mission: map[string]any{"depot": depot, "data": data}})
		return url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_000"}, "inputs": {string(b)}}
	}
	entries := []content.OutcomeEntry{
		{PilotID: pid["Alrek"], TookPart: true}, {PilotID: pid["Katarina"], TookPart: true, Killed: true},
		{PilotID: pid["Jarek"], TookPart: true, MVP: true}, {PilotID: pid["Sybille"], TookPart: false},
	}
	// Preview: 600 SP pool; Alrek 80, Katarina (killed) 0, Jarek 80, Sybille (benched) 40 = 200 from the pool, 400 to the warchest. Jarek, the MVP, earns 20 more
	// for free: it doesn't come out of the pool. Only Alrek and Jarek took part and stay, so only they could be the MVP.
	r := post(t, srv, "/api/campaign/scenario-outcome", report(true, true, entries...), cookie, srv.URL)
	var res content.OutcomeResult
	if r.StatusCode != http.StatusOK || json.NewDecoder(r.Body).Decode(&res) != nil {
		t.Fatalf("preview: %d", r.StatusCode)
	}
	if res.Pool != 600 || sps(res) != "Alrek=80,Katarina=0,Jarek=100,Sybille=40" || res.Warchest != 400 || strings.Join(res.MVPCandidates, ",") != pid["Alrek"]+","+pid["Jarek"] || strings.Join(res.Keywords, ",") != "depot_gone,data_saved" || !res.Pilots[1].KIA {
		t.Fatalf("preview: %+v (%s)", res, sps(res))
	}
	// Every mission has an MVP: a report without one is refused (the preview above works without one, so the dialog can offer the candidates).
	noMVP := append([]content.OutcomeEntry(nil), entries...)
	noMVP[2].MVP = false
	if got := post(t, srv, "/api/campaign/scenario-outcome/commit", report(true, true, noMVP...), cookie, srv.URL).StatusCode; got != http.StatusBadRequest {
		t.Fatalf("a commit without the MVP: %d", got)
	}
	// With a battle in progress, the battle's units are written back into the hangar before the mission is completed: if that fails, nothing is completed.
	if err := store.SaveMekBayOperation(ctx, id, "refusalwar", "scenario_000", storage.MekBayOperation{OpForInstance: "opfor", OperationID: "op-1"}); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.commitErr = errors.New("MekBay is not reachable")
	fake.mu.Unlock()
	if got := post(t, srv, "/api/campaign/scenario-outcome/commit", report(true, true, entries...), cookie, srv.URL).StatusCode; got != http.StatusBadGateway {
		t.Fatalf("a failed write-back refuses the commit: %d", got)
	}
	if done, _ := store.Completions(ctx, id, "refusalwar"); len(done) != 0 {
		t.Fatalf("and nothing is completed: %v", done)
	}
	// The write-back is a hangar edit: while another server holds the hangar's claim it waits, then refuses, and nothing is written or completed.
	fake.mu.Lock()
	fake.commitErr = nil
	fake.mu.Unlock()
	defer func(wait time.Duration) { hangarClaimWait = wait }(hangarClaimWait)
	hangarClaimWait = 50 * time.Millisecond
	token, claimed, err := store.ClaimHangarEdit(ctx, id, "refusalwar")
	if err != nil || !claimed {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	if got := post(t, srv, "/api/campaign/scenario-outcome/commit", report(true, true, entries...), cookie, srv.URL).StatusCode; got != http.StatusConflict {
		t.Fatalf("a write-back while another server edits the hangar: %d", got)
	}
	fake.mu.Lock()
	writeBacks := len(fake.commits)
	fake.mu.Unlock()
	if done, _ := store.Completions(ctx, id, "refusalwar"); len(done) != 0 || writeBacks != 1 {
		t.Fatalf("nothing is written back or completed: %v, %d write-backs", done, writeBacks)
	}
	if err := store.ReleaseHangarEdit(ctx, id, "refusalwar", token); err != nil {
		t.Fatal(err)
	}
	// The hangar as the write-back saved it becomes the cached copy, so the next page shows the battle's damage at once.
	committed := startingHangar()
	committed.Units[0].Locations, committed.Units[0].Damaged = map[string]mekbay.LocationLoss{"CT": {Armor: 7}}, true
	fake.mu.Lock()
	fake.committed = committed
	fake.mu.Unlock()
	if got := post(t, srv, "/api/campaign/scenario-outcome/commit", report(true, true, entries...), cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("commit: %d", got)
	}
	if h, _, ok, err := store.CachedHangar(ctx, id, "refusalwar", run0.HangarInstance); err != nil || !ok || len(h.Units) == 0 || !h.Units[0].Damaged || h.Units[0].Locations["CT"].Armor != 7 {
		t.Errorf("the written-back hangar is cached: ok=%v err=%v %+v", ok, err, h.Units)
	}
	if _, ok, _ := store.ClaimHangarEdit(ctx, id, "refusalwar"); !ok {
		t.Error("the write-back releases its claim on the hangar")
	}
	// Once the mission is completed nothing refers to its battle any more, so it is deleted in MekBay, in the background.
	eventually(t, "the completed mission's battle is deleted in MekBay", func() (bool, any) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return slices.Contains(fake.battleDeletes, "op-1"), fake.battleDeletes
	})
	fake.mu.Lock()
	if len(fake.commits) != 2 || fake.commits[1].OperationID != "op-1" || fake.commits[1].HangarInstance != run0.HangarInstance || fake.commits[1].OwnerUID != mekbay.AccountUID("wolfpack2026", mekbay.SlotHangar) {
		t.Errorf("the hangar was written back from the battle's operation: %+v", fake.commits)
	}
	fake.mu.Unlock()
	after, _ := store.Pilots(ctx, id, "refusalwar")
	for _, p := range after {
		want := map[string]int{"Alrek": 80, "Katarina": 0, "Jarek": 100, "Sybille": 40}[p.Name]
		if p.TotalSP != total[p.Name]+want || p.Fallen != (p.Name == "Katarina") {
			t.Errorf("%s: total %d (was %d, want +%d), fallen %v", p.Name, p.TotalSP, total[p.Name], want, p.Fallen)
		}
	}
	if run, _, _ := store.Run(ctx, id, "refusalwar"); run.Warchest != run0.Warchest+400 || run.FreeRecruits != 0 {
		t.Errorf("400 SP left in the pool go to the warchest: %+v (was %d)", run, run0.Warchest)
	}
	ledger, _ := store.Ledger(ctx, id, "refusalwar")
	if len(ledger) != 2 || ledger[0].Keyword != "data_saved" && ledger[0].Keyword != "depot_gone" {
		t.Errorf("the keywords are in the ledger: %+v", ledger)
	}
	page = body(t, srv, "/refusalwar/scenario_000/", cookie)
	for _, want := range []string{`id="outcome-summary"`, "<b>Jarek</b>", "<b>Sybille</b>", "benched", "&#43;40 SP (benched: half)", "Awarded: 600 SP", "Assigned to pilots: 200 SP", "Warchest: +400 SP", "Keywords granted:", "<b>Depot Gone</b>", "Killed in Action."} {
		if !strings.Contains(page, want) {
			t.Errorf("the report on the page is missing %q", want)
		}
	}
	// The aftermath follows the report (the depot was destroyed), then the Next Steps, each shown once: they are not repeated in the Outcome fold.
	for _, want := range []string{`id="aftermath"`, "The depot burns behind you.", "The Keshik takes note.", "The war goes on without you.", `class="next-steps"`, "Rest, then raid again."} {
		if strings.Count(page, want) != 1 {
			t.Errorf("the completed page should show %q once: %d", want, strings.Count(page, want))
		}
	}
	if strings.Contains(page, "The depot still stands.") {
		t.Error("an aftermath whose condition didn't hold is not shown")
	}
	// The next scenario is open; a second report is refused.
	if !strings.Contains(body(t, srv, "/refusalwar/", cookie), `class="card available" href="/refusalwar/scenario_001/"`) {
		t.Error("Scenario 001 is open")
	}
	if got := post(t, srv, "/api/campaign/scenario-outcome/commit", report(true, true, entries...), cookie, srv.URL).StatusCode; got == http.StatusNoContent {
		t.Error("the outcome can only be committed once")
	}
}

// newTestServerAt serves the campaigns found under root with the given store and MekBay.
func newTestServerAt(t *testing.T, root string, store *storage.Store, mb mekbay.MekBay) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(NewHandler(root, store, WithMekBay(mb)))
	t.Cleanup(srv.Close)
	return srv
}
