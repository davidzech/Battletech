package content

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// A scratch campaign: 000 lists its branches (skipping 001), 001 leaves them out (so leads to 002), 002 names a default_next, 003 is the end of the line.
func scratchCampaign(t *testing.T) *Campaign {
	t.Helper()
	root := t.TempDir()
	write := func(dir, file, text string) {
		p := filepath.Join(root, "Scratch", dir)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, file), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []string{"000", "001", "002", "003", "004"} {
		write("Scenario_"+n, "Setup.md", "---\nid: scenario_"+n+"\ntitle: Mission "+n+"\n---\n# Mission\n")
	}
	write("Scenario_000", "Outcome.md", "---\nid: scenario_000\nbranches: [scenario_002, Scenario_003]\n---\n")
	write("Scenario_001", "Outcome.md", "---\nid: scenario_001\n---\n")
	write("Scenario_002", "Outcome.md", "---\nid: scenario_002\ndefault_next: scenario_004\n---\n")
	write("Scenario_003", "Outcome.md", "---\nid: scenario_003\nbranches: []\n---\n")
	c, ok := (Library{Root: root}).Campaign("scratch")
	if !ok {
		t.Fatal("scratch campaign not loaded")
	}
	return c
}

func TestScenarioBranches(t *testing.T) {
	c := scratchCampaign(t)
	branches := func(slug string) string {
		sc, _ := c.Scenario(slug)
		return strings.Join(sc.Branches, ",")
	}
	for slug, want := range map[string]string{
		"scenario_000": "scenario_002,scenario_003", // listed, lowercased
		"scenario_001": "scenario_002",              // omitted: the next number
		"scenario_002": "scenario_004",              // omitted but with a default_next
		"scenario_003": "",                          // an empty list ends the path
		"scenario_004": "",                          // omitted, but there is no scenario 005
	} {
		if got := branches(slug); got != want {
			t.Errorf("%s branches = %q, want %q", slug, got, want)
		}
	}
	set := func(m map[string]bool) string {
		var out []string
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}
	for name, c2 := range map[string]struct {
		completed []string
		want      string
	}{
		"nothing done: only the first":     {nil, "scenario_000"},
		"000 done: its branches open":      {[]string{"scenario_000"}, "scenario_000,scenario_002,scenario_003"},
		"001 is not reachable from 000":    {[]string{"scenario_000", "scenario_002"}, "scenario_000,scenario_002,scenario_003,scenario_004"},
		"a completed one is always shown":  {[]string{"scenario_001"}, "scenario_000,scenario_001,scenario_002"},
		"an unknown completion is ignored": {[]string{"scenario_999"}, "scenario_000"},
	} {
		done := map[string]bool{}
		for _, s := range c2.completed {
			done[s] = true
		}
		if got := set(c.Available(done)); got != c2.want {
			t.Errorf("%s: %s, want %s", name, got, c2.want)
		}
	}
	// Refusal War: 000 leads to 001, 001 to 002, 002 to 003, and 003 is the end for now.
	rw := refusalWar(t)
	if s0, _ := rw.Scenario("scenario_000"); strings.Join(s0.Branches, ",") != "scenario_001" {
		t.Errorf("Scenario 000 leads to 001: %v", s0.Branches)
	}
	if s1, _ := rw.Scenario("scenario_001"); strings.Join(s1.Branches, ",") != "scenario_002" {
		t.Errorf("Scenario 001 leads to 002: %v", s1.Branches)
	}
	if s2, _ := rw.Scenario("scenario_002"); strings.Join(s2.Branches, ",") != "scenario_003" {
		t.Errorf("Scenario 002 leads to 003: %v", s2.Branches)
	}
	if s3, _ := rw.Scenario("scenario_003"); len(s3.Branches) != 0 {
		t.Errorf("Scenario 003 leads nowhere yet: %v", s3.Branches)
	}
	for _, camp := range []*Campaign{rw, c} {
		for _, sc := range camp.Scenarios {
			for _, b := range sc.Branches {
				if _, ok := camp.Scenario(b); !ok {
					t.Errorf("%s branches to %q, which is not a scenario", sc.Slug, b)
				}
			}
		}
	}
}
