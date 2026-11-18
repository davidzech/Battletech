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
	if button < 0 || button < strings.Index(page, "<summary>Outcome</summary>") || button < strings.Index(page, `id="mekbay-op"`) {
		t.Errorf("Complete mission is at the bottom of the page, after the sections above it (button at %d)", button)
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
