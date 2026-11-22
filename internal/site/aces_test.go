package site

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/content"
)

// The BattleTech: Aces model: objectives met fill a pool; each active pilot takes a flat share of it, a benched pilot half, a killed pilot none; the rest of the pool
// goes to the warchest. Keywords are granted by objectives met or missed.
func acesScenario(t *testing.T, pilotSP any) *content.Scenario {
	t.Helper()
	sc := &content.Scenario{
		OutcomePilotSP: pilotSP,
		OutcomeObjectives: []content.OutcomeObjective{
			{ID: "depot", Label: "Destroy the depot", SP: 400, Grants: []string{"depot_gone"}, UnmetGrants: []string{"depot_standing"}},
			{ID: "escape", Label: "Escape with the data", SP: 200, Grants: []string{"data_saved"}},
		},
	}
	sc.CompileOutcomes()
	if sc.OutcomeError != "" || !sc.HasOutcome() {
		t.Fatalf("an Aces-style scenario: %q", sc.OutcomeError)
	}
	return sc
}

func sps(res content.OutcomeResult) string {
	var out []string
	for _, p := range res.Pilots {
		out = append(out, p.Name+"="+strconv.Itoa(p.SP))
	}
	return strings.Join(out, ",")
}

// Committing an outcome puts its keywords in the campaign ledger (once each), shown on the keywords page.
func TestCampaignLedger(t *testing.T) {
	srv, store, cookie := startedCompany(t)
	ctx := context.Background()
	id := playthroughID("wolfpack2026")
	if p := body(t, srv, "/refusalwar/keywords/", cookie); !strings.Contains(p, "No keywords earned yet.") {
		t.Fatal("an empty ledger says so")
	}
	if err := store.CompleteWithOutcome(ctx, id, "refusalwar", "scenario_000", content.OutcomeResult{Pilots: []content.PilotOutcome{}, Keywords: []string{"depot_gone", "data_saved"}}); err != nil {
		t.Fatal(err)
	}
	ledger, _ := store.Ledger(ctx, id, "refusalwar")
	if len(ledger) != 2 || ledger[0].Scenario != "scenario_000" {
		t.Fatalf("ledger: %+v", ledger)
	}
	// A keyword already held is not recorded twice (a later scenario granting it again leaves the ledger alone).
	if err := store.CompleteWithOutcome(ctx, id, "refusalwar", "scenario_001", content.OutcomeResult{Pilots: []content.PilotOutcome{}, Keywords: []string{"depot_gone"}}); err != nil {
		t.Fatal(err)
	}
	if ledger, _ = store.Ledger(ctx, id, "refusalwar"); len(ledger) != 2 {
		t.Errorf("a keyword is held once: %+v", ledger)
	}
	if p := body(t, srv, "/refusalwar/keywords/", cookie); !strings.Contains(p, `id="ledger"`) || !strings.Contains(p, "<code>depot_gone</code>") || !strings.Contains(p, "<code>data_saved</code>") || strings.Contains(p, "No keywords earned yet.") {
		t.Errorf("the keywords page lists the ledger: %.600s", p)
	}
	if _, err := store.DeleteRun(ctx, id, "refusalwar"); err != nil {
		t.Fatal(err)
	}
	if ledger, _ = store.Ledger(ctx, id, "refusalwar"); len(ledger) != 0 {
		t.Errorf("a deleted campaign keeps no ledger: %+v", ledger)
	}
}
