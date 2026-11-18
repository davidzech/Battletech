package site

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The mission log's status has an LED after it (red until the mission is completed, then green), and completing the mission is the last thing on the page.
func TestMissionLogLayout(t *testing.T) {
	srv, _, cookie := startedCompany(t)
	page := body(t, srv, "/refusalwar/scenario_000/", cookie)
	state := strings.Index(page, `id="mission-log-state"><span>Not completed</span><i class="mission-led" aria-hidden="true"></i>`)
	if state < 0 || !strings.Contains(page, `class="mission-log available"`) {
		t.Fatalf("the status is followed by its LED: %.400s", page)
	}
	button := strings.Index(page, `id="outcome-open"`)
	if button < 0 || button < strings.Index(page, `id="mekbay-op"`) {
		t.Errorf("Complete mission is at the bottom of the page, after the sections above it (button at %d)", button)
	}
	// The outcome text is in the dialog instead of an Outcome dropdown on the page.
	dlg := strings.Index(page, `id="outcome-dialog"`)
	if strings.Contains(page, "<summary>Outcome</summary>") || dlg < 0 || !strings.Contains(page[dlg:], `id="outcome-rules"`) || !strings.Contains(page[dlg:], "Victory &amp; Rewards") || !strings.Contains(page[dlg:], "<td>900</td>") {
		t.Error("a mission with a completion dialog shows its outcome text there, not in a dropdown")
	}
	if strings.Contains(page, "Report how the mission went") {
		t.Error("the dialog's intro paragraph is replaced by the outcome text")
	}
	if !strings.Contains(page, `<section class="mission-actions" id="mission-actions">`) || strings.Index(page, `id="mission-log-error"`) < strings.Index(page, `id="mission-actions"`) {
		t.Error("the button and its error live in the bottom actions section")
	}
	if i := strings.Index(page, `id="mission-log"`); i < 0 || strings.Contains(page[i:i+1200], `id="outcome-open"`) {
		t.Error("the top bar no longer holds the button")
	}

	// Completed: a green LED (the class on the bar), and nothing left to press at the bottom.
	if got := post(t, srv, "/api/campaign/scenario-outcome/commit", url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_000"}, "inputs": {`{"pilots":[],"mission":{}}`}}, cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("commit: %d", got)
	}
	page = body(t, srv, "/refusalwar/scenario_000/", cookie)
	if !strings.Contains(page, `class="mission-log completed"`) || !strings.Contains(page, `<span>Completed &#10003;</span><i class="mission-led"`) || strings.Contains(page, `id="mission-actions"`) {
		t.Error("a completed mission has a green LED and no actions")
	}
	// A plain scenario (no outcome engine) keeps its Mark completed / Mark not completed toggle, also at the bottom.
	if p := body(t, srv, "/refusalwar/scenario_001/", cookie); !strings.Contains(p, `id="mission-actions"`) || !strings.Contains(p, `id="mission-log-toggle">Mark completed`) || !strings.Contains(p, `class="mission-log available"`) {
		t.Errorf("Scenario 001: %.300s", p)
	}
}

// Pages without a completion dialog keep the Outcome dropdown, since nothing else would show their outcome text.
func TestOutcomeDropdownRemainsWithoutADialog(t *testing.T) {
	srv, _, cookie := startedCompany(t)
	if p := body(t, srv, "/refusalwar/scenario_001/", cookie); !strings.Contains(p, "<summary>Outcome</summary>") || strings.Contains(p, `id="outcome-dialog"`) {
		t.Error("Scenario 001 has no outcome engine, so its Outcome dropdown stays")
	}
	// Once Scenario 000 is completed (no dialog left) its outcome text is back in the dropdown.
	if got := post(t, srv, "/api/campaign/scenario-outcome/commit", url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_000"}, "inputs": {`{"pilots":[],"mission":{}}`}}, cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("commit: %d", got)
	}
	if p := body(t, srv, "/refusalwar/scenario_000/", cookie); !strings.Contains(p, "<summary>Outcome</summary>") || strings.Contains(p, `id="outcome-dialog"`) {
		t.Error("a completed mission shows its outcome text in the dropdown again")
	}
	// Before the campaign is started there is no dialog either.
	other := post(t, srv, "/api/seed", url.Values{"seed": {"freshseed001"}}, nil, "").Cookies()[0]
	if p := body(t, srv, "/refusalwar/scenario_000/", other); !strings.Contains(p, "<summary>Outcome</summary>") {
		t.Error("without a started campaign the Outcome dropdown stays")
	}
}
