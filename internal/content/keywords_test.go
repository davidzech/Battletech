package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Keywords.md's front matter is the registry: each keyword's ID, name, source scenario and description. A scenario can
// grant only keywords registered there, and only its own; a malformed entry is a campaign problem.
func TestKeywordRegistry(t *testing.T) {
	root := t.TempDir()
	write := func(path, text string) {
		p := filepath.Join(root, "Scratch", path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Keywords.md", `---
keywords:
  - id: depot_gone
    name: Depot Gone
    scenario: Scenario_000
    description: The depot was destroyed.
  - id: later_one
    name: Later One
    scenario: scenario_009
    description: ""
  - id: Bad_Case
    name: Bad
    scenario: scenario_000
  - id: depot_gone
    name: Twice
    scenario: scenario_000
  - id: no_name
    scenario: scenario_000
  - id: no_scenario
    name: No Scenario
---

# Keywords

Notes for authors.
`)
	write("Scenario_000/Setup.md", "---\nid: scenario_000\ntitle: Good\n---\n# Good\n")
	write("Scenario_000/Outcome.md", "---\nid: scenario_000\npilot_sp: 50\nobjectives:\n  - id: depot\n    sp: 300\n    grants: [depot_gone]\n---\n")
	write("Scenario_001/Setup.md", "---\nid: scenario_001\ntitle: Bad\n---\n# Bad\n")
	write("Scenario_001/Outcome.md", "---\nid: scenario_001\nobjectives:\n  - id: depot\n    sp: 300\n    grants: [made_up_keyword]\n---\n")
	write("Scenario_002/Setup.md", "---\nid: scenario_002\ntitle: Thief\n---\n# Thief\n")
	write("Scenario_002/Outcome.md", "---\nid: scenario_002\nobjectives:\n  - id: depot\n    sp: 300\n    grants: [later_one]\n---\n")
	c, ok := (Library{Root: root}).Campaign("scratch")
	if !ok {
		t.Fatal("the campaign loads")
	}
	var ids []string
	for _, k := range c.Keywords {
		ids = append(ids, k.ID)
	}
	if got := strings.Join(ids, ","); got != "depot_gone,later_one" {
		t.Errorf("the registry keeps the good entries: %q", got)
	}
	if k, _ := c.Keyword("depot_gone"); k.Name != "Depot Gone" || k.Scenario != "scenario_000" || k.Description != "The depot was destroyed." {
		t.Errorf("depot_gone = %+v", k)
	}
	problems := strings.Join(c.Problems, "\n")
	for _, want := range []string{`"Bad_Case", which is not lowercase snake_case`, `"depot_gone" is listed twice`, `"no_name" has no name`, `"no_scenario" has the scenario ""`} {
		if !strings.Contains(problems, want) {
			t.Errorf("problems are missing %q:\n%s", want, problems)
		}
	}
	// Players see the name; a keyword the registry doesn't know (still in an old ledger) reads as its ID in words.
	if a, b := c.KeywordName("depot_gone"), c.KeywordName("data_saved"); a != "Depot Gone" || b != "Data Saved" {
		t.Errorf("KeywordName = %q, %q", a, b)
	}
	good, _ := c.Scenario("scenario_000")
	bad, _ := c.Scenario("scenario_001")
	thief, _ := c.Scenario("scenario_002")
	if !good.HasOutcome() || good.OutcomeError != "" {
		t.Errorf("a scenario granting its own keyword is fine: %q", good.OutcomeError)
	}
	if bad.HasOutcome() || !strings.Contains(bad.OutcomeError, "made_up_keyword") {
		t.Errorf("an unregistered keyword is refused: %q", bad.OutcomeError)
	}
	if thief.HasOutcome() || !strings.Contains(thief.OutcomeError, "which Keywords.md gives to scenario_009") {
		t.Errorf("another scenario's keyword is refused: %q", thief.OutcomeError)
	}
}
