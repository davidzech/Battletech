package site

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	write("Keywords.md", "# Keywords\n\n- `depot_gone` - the depot was destroyed\n- `depot_standing` - the depot still stands\n- `data_saved` - the data got out\n")
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
---
`)
	write("Scenario_001/Setup.md", "---\nid: scenario_001\ntitle: Next\n---\n# Next\n")

	fake := &fakeMekBay{hangar: startingHangar()}
	store, err := OpenSQLite(t.TempDir() + "/aces.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.db.Close() })
	srv := newTestServerAt(t, root, store, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	id := playthroughID("wolfpack2026")
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}, "uid_p1": {uidP1}}, cookie, srv.URL)
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
	report := func(depot, data bool, entries ...OutcomeEntry) url.Values {
		b, _ := json.Marshal(OutcomeInputs{Pilots: entries, Mission: map[string]any{"depot": depot, "data": data}})
		return url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_000"}, "inputs": {string(b)}}
	}
	entries := []OutcomeEntry{
		{PilotID: pid["Alrek"], TookPart: true}, {PilotID: pid["Katarina"], TookPart: true, Killed: true},
		{PilotID: pid["Jarek"], TookPart: true}, {PilotID: pid["Sybille"], TookPart: false},
	}
	// Preview: 600 SP pool; Alrek 80, Katarina (killed) 0, Jarek 80, Sybille (benched) 40 = 200 from the pool, 400 to the warchest.
	r := post(t, srv, "/api/campaign/scenario-outcome", report(true, true, entries...), cookie, srv.URL)
	var res OutcomeResult
	if r.StatusCode != http.StatusOK || json.NewDecoder(r.Body).Decode(&res) != nil {
		t.Fatalf("preview: %d", r.StatusCode)
	}
	if res.Pool != 600 || sps(res) != "Alrek=80,Katarina=0,Jarek=80,Sybille=40" || res.Warchest != 400 || strings.Join(res.Keywords, ",") != "depot_gone,data_saved" || !res.Pilots[1].KIA {
		t.Fatalf("preview: %+v (%s)", res, sps(res))
	}
	if got := post(t, srv, "/api/campaign/scenario-outcome/commit", report(true, true, entries...), cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("commit: %d", got)
	}
	after, _ := store.Pilots(ctx, id, "refusalwar")
	for _, p := range after {
		want := map[string]int{"Alrek": 80, "Katarina": 0, "Jarek": 80, "Sybille": 40}[p.Name]
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
	for _, want := range []string{`id="outcome-summary"`, "<b>Jarek</b>", "<b>Sybille</b>", "benched", "&#43;40 SP (benched: half)", "Pool: 600 SP. 200 to the pilots, 400 to the warchest.", "Keywords granted:", "<code>depot_gone</code>", "Killed in Action."} {
		if !strings.Contains(page, want) {
			t.Errorf("the report on the page is missing %q", want)
		}
	}
	if p := body(t, srv, "/refusalwar/keywords/", cookie); !strings.Contains(p, "<code>depot_gone</code>") || !strings.Contains(p, "<code>data_saved</code>") || strings.Contains(p, "<code>depot_standing</code> <span") {
		t.Errorf("the keywords page shows what was earned, not what was not: %.500s", p)
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
func newTestServerAt(t *testing.T, root string, store *Store, mb MekBay) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(NewHandler(root, store, WithMekBay(mb)))
	t.Cleanup(srv.Close)
	return srv
}
