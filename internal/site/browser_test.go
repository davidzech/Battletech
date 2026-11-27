package site

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/storage"
)

// TestBrowser runs the Playwright suite in browsertests/ (the browser scripts' tests) against this site, served with the fake MekBay. It only runs when
// BROWSER_TESTS is set, and needs Node with the suite's packages and Chromium installed (cd browsertests && npm ci && npx playwright install chromium).
// BROWSER_TESTS_ARGS passes more arguments to "playwright test", e.g. a file name or --headed.
//
// Besides the site, the server answers two test-only endpoints the suite uses to set the scene: POST /__test/hangar (a mekbay.HangarForce as JSON) sets
// the hangar the fake MekBay answers, and POST /__test/battle (a mekbay.BattleStatus as JSON) the battle it reports. The fake is shared by every account,
// so the suite runs one test at a time.
func TestBrowser(t *testing.T) {
	if os.Getenv("BROWSER_TESTS") == "" {
		t.Skip("set BROWSER_TESTS=1 to run the Playwright suite in browsertests/")
	}
	npx, err := exec.LookPath("npx")
	if err != nil {
		t.Fatal("the browser tests need Node (npx): ", err)
	}
	store, err := storage.OpenSQLite(t.TempDir() + "/browser.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	fake := &fakeMekBay{hangar: startingHangar()}

	mux := http.NewServeMux()
	mux.Handle("/", NewHandler(browserCampaigns(t), store, WithMekBay(browserMekBay{fake})))
	mux.HandleFunc("POST /__test/hangar", func(w http.ResponseWriter, r *http.Request) {
		var h mekbay.HangarForce
		if err := strictJSON(r, &h); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fake.setHangar(h, nil)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /__test/battle", func(w http.ResponseWriter, r *http.Request) {
		var s mekbay.BattleStatus
		if err := strictJSON(r, &s); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fake.mu.Lock()
		fake.status = s
		fake.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	args := append([]string{"playwright", "test"}, strings.Fields(os.Getenv("BROWSER_TESTS_ARGS"))...)
	cmd := exec.Command(npx, args...)
	cmd.Dir = "../../browsertests"
	cmd.Env = append(os.Environ(), "BASE_URL="+srv.URL)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("playwright test: %v", err)
	}
}

// strictJSON decodes a test endpoint's body, refusing fields the type doesn't have, so a test written against a renamed field fails instead of silently
// setting nothing.
func strictJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// browserMekBay is the fake MekBay as the browser tests use it: a hangar built on the site (the start wizard's Build your force) becomes the hangar it
// answers, as MekBay would save it.
type browserMekBay struct{ *fakeMekBay }

func (b browserMekBay) CreateForce(ctx context.Context, owner, instance string, d mekbay.ForceDraft) (mekbay.HangarForce, error) {
	h, err := b.fakeMekBay.CreateForce(ctx, owner, instance, d)
	if err == nil {
		b.setHangar(h, nil)
	}
	return h, err
}

// browserCampaigns is the real campaigns plus Drill, a small campaign whose Scenario 000 has no forces and an Aces-style outcome (an objective, pilot SP and
// an MVP), so its Complete mission dialog opens straight from the page, with the Active and Killed in action boxes, rather than from a battle.
func browserCampaigns(t *testing.T) string {
	t.Helper()
	intro, err := os.ReadFile("../../campaigns/RefusalWar/Intro.md")
	if err != nil {
		t.Fatal(err)
	}
	return campaignsCopy(t, map[string]string{
		"Drill/Intro.md":              string(intro),
		"Drill/Keywords.md":           "---\nkeywords:\n  - {id: depot_gone, name: Depot Gone, scenario: scenario_000, description: The depot was destroyed.}\n---\n",
		"Drill/Scenario_000/Setup.md": "---\nid: scenario_000\ntitle: Raid\nplanet: Colmar\n---\n# Scenario 000: Raid\n\n## Briefing\n\nRaid the depot.\n",
		"Drill/Scenario_000/Outcome.md": `---
id: scenario_000
branches: []
pilot_sp: 80
objectives:
  - id: depot
    label: Destroy the depot
    sp: 400
    grants: [depot_gone]
---
# Scenario 000: Raid - Outcome

## Victory & Rewards

80 SP to each pilot from the objectives' pool.
`,
	})
}
