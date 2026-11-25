package site

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/content"
)

func TestRefusalWarPlayersOnHomePage(t *testing.T) {
	c, ok := (content.Library{Root: "../../campaigns"}).Campaign("refusalwar")
	if !ok || c.MinPlayers != 1 || c.MaxPlayers != 2 || c.PlayersLabel() != "1-2 players" {
		t.Fatalf("Refusal War should be for 1-2 players: %+v", c)
	}
	srv, _ := newTestServer(t)
	home := body(t, srv, "/", nil)
	if !strings.Contains(home, `<span class="sub">1-2 players</span>`) {
		t.Fatalf("the campaigns list should show the player range: %.1500s", home)
	}
	// Each campaign card shows the campaign's emblem and its era, with the era's icon.
	for _, want := range []string{`<img src="/refusalwar/images/icon.webp"`, `<span class="era"><img src="/static/era-clan-invasion.webp" alt="" width="20" height="20">Clan Invasion Era</span>`} {
		if !strings.Contains(home, want) {
			t.Errorf("the campaign card is missing %q", want)
		}
	}
	for path, want := range map[string]int{"/refusalwar/images/icon.webp": http.StatusOK, "/static/era-clan-invasion.webp": http.StatusOK, "/refusalwar/images/nope.webp": http.StatusNotFound} {
		if res := get(t, srv, path, nil); res.StatusCode != want {
			t.Errorf("%s: %d, want %d", path, res.StatusCode, want)
		}
	}
	if c.Icon != "icon.webp" || c.AvailabilityEra != "Clan Invasion" {
		t.Errorf("Refusal War's icon and era: %q %q", c.Icon, c.AvailabilityEra)
	}
}

// An Intro.md icon must be a file in the campaign's images/ folder.
func TestCampaignIconProblems(t *testing.T) {
	for icon, want := range map[string]string{"images/missing.webp": "is not in the campaign's folder", "../Intro.md": "must be a file in the campaign's images/ folder", "images/sub/icon.webp": "must be a file"} {
		root := t.TempDir()
		dir := filepath.Join(root, "Scratch")
		for _, d := range []string{"Scenario_000", "images"} {
			if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		_ = os.WriteFile(filepath.Join(dir, "Intro.md"), []byte("---\nicon: "+icon+"\n---\n"), 0o644)
		_ = os.WriteFile(filepath.Join(dir, "Scenario_000", "Setup.md"), []byte("---\nid: scenario_000\ntitle: First\n---\n# First\n"), 0o644)
		c, ok := (content.Library{Root: root}).Campaign("scratch")
		if !ok {
			t.Fatal("the campaign loads")
		}
		if c.Icon != "" || !strings.Contains(strings.Join(c.Problems, "\n"), want) {
			t.Errorf("icon %q: %q, problems %v", icon, c.Icon, c.Problems)
		}
	}
}

// A campaign names its scenarios with its mission_term ("Scenario" by default, which Refusal War uses), and that name
// is used everywhere a scenario is named.
func TestMissionTerm(t *testing.T) {
	c := refusalWar(t)
	sc, _ := c.Scenario("scenario_001")
	if c.MissionNumber(*sc) != "Scenario 001" || c.MissionName(*sc) != "Scenario 001: Pyre Light" || operationName(c, sc) != "Refusal War - Scenario 001" {
		t.Errorf("names: %q %q %q", c.MissionNumber(*sc), c.MissionName(*sc), operationName(c, sc))
	}
	custom := *c
	custom.MissionTerm = "Sortie"
	if custom.MissionName(*sc) != "Sortie 001: Pyre Light" {
		t.Errorf("a campaign's own term: %q", custom.MissionName(*sc))
	}
	srv, _, cookie := startedCompany(t)
	if p := body(t, srv, "/refusalwar/scenario_001/", cookie); !strings.Contains(p, `<p class="kicker">Scenario 001`) {
		t.Errorf("the scenario page's kicker: %.300s", p)
	}
	if p := body(t, srv, "/refusalwar/manage/?back=scenario_001", cookie); !strings.Contains(p, "Back to Scenario 001: Pyre Light") {
		t.Error("the Manage page's back link names the scenario")
	}
}

// The Deploy section links to Manage force with ?back=<scenario>, and Manage's back link then returns to that scenario. Only a real scenario of the campaign counts.
func TestManageBackLinkReturnsToTheScenario(t *testing.T) {
	srv, _, cookie := startedCompany(t)
	scenario := body(t, srv, "/refusalwar/scenario_001/", cookie)
	if !strings.Contains(scenario, `id="manage-link" href="/refusalwar/manage/?back=scenario_001"`) {
		t.Fatalf("the Deploy section should link to Manage force carrying the scenario: %.600s", scenario)
	}
	// A scenario that provides its own mechs has no hangar to manage for it.
	if strings.Contains(body(t, srv, "/refusalwar/scenario_000/", cookie), `id="manage-link"`) {
		t.Error("a scenario with provided mechs should not offer Manage force")
	}
	man := body(t, srv, "/refusalwar/manage/?back=scenario_001", cookie)
	if !strings.Contains(man, `<a href="/refusalwar/scenario_001/">&lsaquo; Back to Scenario 001: `) {
		t.Errorf("Back should return to the scenario: %.400s", man)
	}
	if plain := body(t, srv, "/refusalwar/manage/", cookie); !strings.Contains(plain, `<a href="/refusalwar/">&lsaquo; Back to the campaign</a>`) {
		t.Error("without ?back, Back returns to the campaign")
	}
	for _, bad := range []string{"https://evil.example/", "//evil.example", "../scenario_001", "scenario_999", "scenario_001/../x", "%0d%0a"} {
		page := body(t, srv, "/refusalwar/manage/?back="+bad, cookie)
		if !strings.Contains(page, `<a href="/refusalwar/">&lsaquo; Back to the campaign</a>`) || strings.Contains(page, "evil.example") {
			t.Errorf("?back=%s must be ignored", bad)
		}
	}
}

// The mission log's status has an LED after it (red until the mission is completed, then green), and completing the mission is the last thing on the page.
func TestMissionLogLayout(t *testing.T) {
	srv, _, cookie := startedCompany(t)
	page := body(t, srv, "/refusalwar/scenario_000/", cookie)
	state := strings.Index(page, `id="mission-log-state"><span>Not completed</span><i class="mission-led" aria-hidden="true"></i>`)
	if state < 0 || !strings.Contains(page, `class="mission-log available"`) {
		t.Fatalf("the status is followed by its LED: %.400s", page)
	}
	// Scenario 000 has an OpFor to deploy against, so Complete mission lives in the Deploy dialog's Operation in progress view, with Abort mission tucked under it.
	button, actions, abort := strings.Index(page, `id="outcome-open"`), strings.Index(page, `id="op-actions"`), strings.Index(page, `id="abort-open"`)
	if actions < 0 || button < actions || abort < button || strings.Index(page, `id="deploy-dialog"`) > actions {
		t.Errorf("Complete mission and then Abort mission are in the operation view of the Deploy dialog (%d %d %d)", actions, button, abort)
	}
	if !strings.Contains(page, `<details class="op-abort" id="op-abort"><summary>More options</summary>`) || !strings.Contains(page, `class="button button-outline button-large danger" id="abort-open">Abort mission</button>`) {
		t.Error("Abort mission is hidden under More options and is the same button in red")
	}
	// The dialog does not repeat the Victory & Rewards text; the Outcome dropdown comes back once the mission is completed.
	dlg := strings.Index(page, `id="outcome-dialog"`)
	if strings.Contains(page, "<summary>Outcome</summary>") || dlg < 0 || strings.Contains(page[dlg:], `id="outcome-rules"`) || strings.Contains(page[dlg:], "Victory &amp; Rewards") {
		t.Error("a mission with a completion dialog has no Outcome dropdown and does not repeat its rewards text in the dialog")
	}
	if strings.Contains(page, "Report how the mission went") {
		t.Error("the dialog's intro paragraph is replaced by the outcome text")
	}
	if strings.Contains(page, `id="mission-actions"`) {
		t.Error("a scenario with a Deploy section has no actions at the bottom of the page")
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

// The scenario page no longer ends with a "Next: <title>" link (a different system will replace it).
func TestNoNextScenarioLink(t *testing.T) {
	srv, _, cookie := startedCompany(t)
	for _, path := range []string{"/refusalwar/scenario_000/", "/refusalwar/scenario_001/"} {
		if p := body(t, srv, path, cookie); strings.Contains(p, `class="next"`) || strings.Contains(p, "Next: ") {
			t.Errorf("%s still has a Next link", path)
		}
	}
}

// Buttons never end in an ellipsis ("Deploy force", not "Deploy force..."), and Complete mission is the big outline button.
func TestButtonsHaveNoTrailingEllipsis(t *testing.T) {
	ellipsis := regexp.MustCompile(`(?s)<(button|a)\b[^>]*class="[^"]*\bbutton\b[^"]*"[^>]*>[^<]*(&hellip;|…|\.\.\.)\s*</(button|a)>`)
	srv, _, cookie := startedCompany(t)
	fresh := post(t, srv, "/api/seed", url.Values{"seed": {"freshseed002"}}, nil, "").Cookies()[0]
	for name, c := range map[string]struct {
		path   string
		cookie bool
	}{
		"the campaign page":     {"/refusalwar/", true},
		"the manage page":       {"/refusalwar/manage/", true},
		"a scenario (outcome)":  {"/refusalwar/scenario_000/", true},
		"a scenario (plain)":    {"/refusalwar/scenario_001/", true},
		"an unstarted campaign": {"/refusalwar/", false},
		"an unstarted scenario": {"/refusalwar/scenario_000/", false},
		"the home page":         {"/", true},
	} {
		ck := fresh
		if c.cookie {
			ck = cookie
		}
		if m := ellipsis.FindString(body(t, srv, c.path, ck)); m != "" {
			t.Errorf("%s has a button ending in an ellipsis: %s", name, m)
		}
	}
	page := body(t, srv, "/refusalwar/scenario_000/", cookie)
	if !strings.Contains(page, `class="button button-outline button-large" id="outcome-open">Complete mission</button>`) {
		t.Errorf("Complete mission is the big outline button: %.200s", page)
	}
	css := body(t, srv, "/static/style.css", nil)
	for _, want := range []string{".button.button-outline, button.button.button-outline { background: transparent; color: var(--accent); border: 1px solid var(--accent); clip-path: none; }", "border-width: 3px", ".mission-actions { display: flex; flex-wrap: wrap; align-items: center; justify-content: center;"} {
		if !strings.Contains(css, want) {
			t.Errorf("the stylesheet is missing %q", want)
		}
	}
}

func sps(res content.OutcomeResult) string {
	var out []string
	for _, p := range res.Pilots {
		out = append(out, p.Name+"="+strconv.Itoa(p.SP))
	}
	return strings.Join(out, ",")
}

// Committing an outcome puts its keywords in the campaign ledger (once each), listed on the campaign overview by name.
func TestCampaignLedger(t *testing.T) {
	srv, store, cookie := startedCompany(t)
	ctx := context.Background()
	id := playthroughID("wolfpack2026")
	if ledger, _ := store.Ledger(ctx, id, "refusalwar"); len(ledger) != 0 {
		t.Fatalf("a new campaign holds no keywords: %+v", ledger)
	}
	if p := body(t, srv, "/refusalwar/", cookie); !strings.Contains(p, `id="force-keywords"`) || !strings.Contains(p, "No keywords earned yet.") {
		t.Error("the overview says when no keywords are earned")
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
	if err := store.CompleteWithOutcome(ctx, id, "refusalwar", "scenario_002", content.OutcomeResult{Pilots: []content.PilotOutcome{}, Keywords: []string{"scorched_earth"}}); err != nil {
		t.Fatal(err)
	}
	p := body(t, srv, "/refusalwar/", cookie)
	for _, want := range []string{`data-from="Scenario 000: Trial of Position">Depot Gone</li>`, `>Data Saved</li>`, `data-text="The chemical plant was completely destroyed."`, `>Scorched Earth</li>`} {
		if !strings.Contains(p, want) {
			t.Errorf("the overview lists the earned keywords by name with their descriptions: missing %q", want)
		}
	}
	if strings.Contains(p, "scorched_earth") || strings.Contains(p, "No keywords earned yet.") {
		t.Error("the overview shows keyword names, never IDs")
	}
	if _, err := store.DeleteRun(ctx, id, "refusalwar"); err != nil {
		t.Fatal(err)
	}
	if ledger, _ = store.Ledger(ctx, id, "refusalwar"); len(ledger) != 0 {
		t.Errorf("a deleted campaign keeps no ledger: %+v", ledger)
	}
}

// A started campaign's intro is folded with a Show more button (introfold.js); before the start it is shown whole, since it is what the player reads first.
func TestStartedCampaignFoldsTheIntro(t *testing.T) {
	srv, _ := newTestServer(t)
	if p := body(t, srv, "/refusalwar/", nil); strings.Contains(p, "intro-collapsible") || strings.Contains(p, "introfold.js") || !strings.Contains(p, `id="campaign-intro-text"`) {
		t.Errorf("an unstarted campaign shows its intro whole: %.600s", p)
	}
	srv, _, cookie := startedCompany(t)
	p := body(t, srv, "/refusalwar/", cookie)
	for _, want := range []string{`class="briefing campaign-intro intro-collapsible"`, `class="intro-more" aria-expanded="false" aria-controls="campaign-intro-text" hidden>Show more</button>`, `<script src="/static/introfold.js" defer>`} {
		if !strings.Contains(p, want) {
			t.Errorf("a started campaign folds its intro: missing %q", want)
		}
	}
}

// Every page loads the wizards' keyboard shortcuts (wizardkeys.js: Enter for Next, Backspace for Back), and each wizard marks the buttons they press.
func TestWizardsMarkTheirNextAndBackButtons(t *testing.T) {
	srv, _ := newTestServer(t)
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	start := body(t, srv, "/refusalwar/", cookie)
	for _, want := range []string{`<script src="/static/wizardkeys.js" defer></script>`, `id="wiz-back" data-wizard-back>`, `id="wiz-next" data-wizard-next>`, `id="setup-back" data-wizard-back>`} {
		if !strings.Contains(start, want) {
			t.Errorf("the start wizard's page is missing %q", want)
		}
	}
	srv, _, cookie = startedCompany(t)
	deploy := body(t, srv, "/refusalwar/scenario_001/", cookie)
	for _, want := range []string{`<script src="/static/wizardkeys.js" defer></script>`, `id="deploy-back" data-wizard-back hidden>`, `id="deploy-next" data-wizard-next hidden disabled>`} {
		if !strings.Contains(deploy, want) {
			t.Errorf("the Deploy wizard's page is missing %q", want)
		}
	}
}

// The campaign's emblem is a crest beside the title of the campaign page and every page under it (each scenario, the Manage page), not of the home page.
func TestCampaignPagesShowTheCrest(t *testing.T) {
	const crest = `<div class="page-head"><img class="page-crest" src="/refusalwar/images/icon.webp" alt="">`
	srv, _, cookie := startedCompany(t)
	for _, path := range []string{"/refusalwar/", "/refusalwar/scenario_000/", "/refusalwar/scenario_001/", "/refusalwar/manage/"} {
		if !strings.Contains(body(t, srv, path, cookie), crest) {
			t.Errorf("%s shows the campaign's crest", path)
		}
	}
	if strings.Contains(body(t, srv, "/", cookie), "page-crest") {
		t.Error("the home page has no campaign crest")
	}
}
