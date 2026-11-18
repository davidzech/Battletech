package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every campaign's files load cleanly: front matter with only known keys, outcome rules that compile, branches to real scenarios. A new campaign or scenario
// with a typo fails here (and shows its problems on its campaign page).
func TestCampaignFilesAreValid(t *testing.T) {
	cs, err := (Library{Root: "../../campaigns"}).Campaigns()
	if err != nil || len(cs) == 0 {
		t.Fatalf("campaigns: %v", err)
	}
	for _, c := range cs {
		for _, p := range c.Problems {
			t.Errorf("%s: %s", c.Slug, p)
		}
	}
}

// A campaign's problems are found and listed rather than silently ignored: an unknown key, a key in the wrong file, an outcome that doesn't compile and a branch
// to nowhere.
func TestCampaignProblemsAreReported(t *testing.T) {
	root := t.TempDir()
	write := func(path, text string) {
		p := filepath.Join(root, "Test", path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Intro.md", "---\nmax_players: 2\nmax_playerz: 3\n---\n")
	write("Scenario_000/Setup.md", "---\nid: scenario_000\ntitle: First\nbv_limt: 9000\n---\n# First\n")
	write("Scenario_000/Outcome.md", "---\nbranches: [scenario_009]\npilot_sp: 10 +\n---\n")
	write("Scenario_001/Setup.md", "---\ntitle: Second\nbranches: [scenario_000]\n---\n# Second\n")
	c, ok := (Library{Root: root}).Campaign("test")
	if !ok {
		t.Fatal("the campaign still loads")
	}
	all := strings.Join(c.Problems, "\n")
	for _, want := range []string{"Intro.md", "max_playerz", "Scenario_000/Setup.md", "bv_limt", "scenario_000/Outcome.md", "scenario_009", "Scenario_001/Setup.md", "branches"} {
		if !strings.Contains(all, want) {
			t.Errorf("the problems should mention %q:\n%s", want, all)
		}
	}
	if c.MaxPlayers != 0 && c.MaxPlayers != 2 {
		t.Errorf("max_players: %d", c.MaxPlayers)
	}
	if c.MissionTerm != "Scenario" || c.MissionName(c.Scenarios[1]) != "Scenario 001: Second" {
		t.Errorf("the default mission term: %q %q", c.MissionTerm, c.MissionName(c.Scenarios[1]))
	}
}

// Refusal War calls its scenarios sorties, and that name is used everywhere a scenario is named.
func TestMissionTerm(t *testing.T) {
	c := refusalWar(t)
	sc, _ := c.Scenario("scenario_001")
	if c.MissionNumber(*sc) != "Sortie 001" || c.MissionName(*sc) != "Sortie 001: Pyre Light" || operationName(c, sc) != "Refusal War - Sortie 001" {
		t.Errorf("names: %q %q %q", c.MissionNumber(*sc), c.MissionName(*sc), operationName(c, sc))
	}
	srv, _, cookie := startedCompany(t)
	if p := body(t, srv, "/refusalwar/scenario_001/", cookie); !strings.Contains(p, `<p class="kicker">Sortie 001`) {
		t.Errorf("the scenario page's kicker: %.300s", p)
	}
	if p := body(t, srv, "/refusalwar/manage/?back=scenario_001", cookie); !strings.Contains(p, "Back to Sortie 001: Pyre Light") {
		t.Error("the Manage page's back link names the sortie")
	}
}
