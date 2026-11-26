package content

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

// A scenario's id is its folder name in lowercase: a Setup.md or Outcome.md whose id says otherwise (say, copied from another scenario) is a campaign
// problem, and the scenario keeps its folder's ID, so no two scenarios share one and battles can't be looked up under the wrong scenario.
func TestScenarioIDMustMatchItsFolder(t *testing.T) {
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
	write("Intro.md", "---\nmax_players: 2\n---\n")
	write("Scenario_000/Setup.md", "---\nid: scenario_000\ntitle: First\n---\n# First\n")
	write("Scenario_000/Outcome.md", "---\nid: scenario_000\n---\n")
	write("Scenario_001/Setup.md", "---\nid: scenario_000\ntitle: Copied\n---\n# Copied\n")
	write("Scenario_001/Outcome.md", "---\nid: scenario_007\n---\n")
	write("Scenario_002/Setup.md", "---\nid: Scenario_002\ntitle: Capitals\n---\n# Capitals\n")
	write("Scenario_003/Setup.md", "---\ntitle: No id\n---\n# No id\n")
	c, ok := (Library{Root: root}).Campaign("test")
	if !ok {
		t.Fatal("the campaign still loads")
	}
	want := []string{
		`scenario_001/Setup.md: id "scenario_000" must be the folder's name in lowercase, "scenario_001"`,
		`scenario_001/Outcome.md: id "scenario_007" must be the folder's name in lowercase, "scenario_001"`,
		`scenario_002/Setup.md: id "Scenario_002" must be the folder's name in lowercase, "scenario_002"`,
	}
	if strings.Join(c.Problems, "\n") != strings.Join(want, "\n") {
		t.Errorf("problems:\n%s\nwant:\n%s", strings.Join(c.Problems, "\n"), strings.Join(want, "\n"))
	}
	for _, sc := range c.Scenarios {
		if sc.ID != sc.Slug {
			t.Errorf("%s has the ID %q", sc.Slug, sc.ID)
		}
		if got, ok := c.ScenarioByID(sc.Slug); !ok || got.Slug != sc.Slug {
			t.Errorf("ScenarioByID(%q) = %+v, %v", sc.Slug, got, ok)
		}
	}
}
