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
		"nothing done: only the first":         {nil, "scenario_000"},
		"000 done: its branches open":          {[]string{"scenario_000"}, "scenario_000,scenario_002,scenario_003"},
		"a choice closes once one is made":     {[]string{"scenario_000", "scenario_002"}, "scenario_000,scenario_002,scenario_004"}, // 003 was the other choice
		"the last completed (highest) decides": {[]string{"scenario_000", "scenario_002", "scenario_004"}, "scenario_000,scenario_002,scenario_004"},
		"a path's end offers nothing new":      {[]string{"scenario_000", "scenario_003"}, "scenario_000,scenario_003"},
		"completed ones are always shown":      {[]string{"scenario_001"}, "scenario_001,scenario_002"},
		"an unknown completion is ignored":     {[]string{"scenario_999"}, "scenario_000"},
	} {
		done := map[string]bool{}
		for _, s := range c2.completed {
			done[s] = true
		}
		if got := set(c.Available(done)); got != c2.want {
			t.Errorf("%s: %s, want %s", name, got, c2.want)
		}
	}
	// Refusal War's paths cross and differ in length, but every one runs from 000 to the Epilogue, 013, always forward, in 6 to 8 sorties.
	rw := refusalWar(t)
	var walk func(id string, path []string)
	paths := 0
	walk = func(id string, path []string) {
		path = append(path, id)
		s, ok := rw.Scenario(id)
		if !ok {
			t.Fatalf("%v: %s is not a scenario", path, id)
		}
		if len(s.Branches) == 0 {
			paths++
			if id != "scenario_013" || len(path) < 6 || len(path) > 8 {
				t.Errorf("a run should end at the Epilogue after 6 to 8 sorties: %v", path)
			}
			return
		}
		for _, b := range s.Branches {
			if b <= id {
				t.Errorf("%s branches back to %s", id, b)
				continue
			}
			walk(b, path)
		}
	}
	walk("scenario_000", nil)
	if paths < 10 {
		t.Errorf("only %d paths", paths)
	}
	if s1, _ := rw.Scenario("scenario_001"); strings.Join(s1.Branches, ",") != "scenario_002,scenario_003" {
		t.Errorf("Scenario 001 offers a choice of 002 or 003: %v", s1.Branches)
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
