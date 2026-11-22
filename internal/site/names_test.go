package site

import (
	"encoding/json"
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A campaign's Names.md gives its own example names; only the lists it gives are sent, a name a player couldn't type is a problem, and a campaign without the
// file keeps the baseline (an empty object).
func TestCampaignNames(t *testing.T) {
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
	write("Scenario_000/Setup.md", "---\ntitle: First\n---\n")
	c, _ := (Library{Root: root}).Campaign("test")
	if got := c.NamesJSON(); got != "{}" || len(c.Problems) != 0 {
		t.Errorf("no Names.md: %s %v", got, c.Problems)
	}

	write("Names.md", "---\nforce_names: [Iron Jaw Binary, \"<bad>\"]\nfemale_pilot_names: [Ranna]\n---\n# Names\n")
	c, _ = (Library{Root: root}).Campaign("test")
	var got map[string][]string
	if err := json.Unmarshal([]byte(c.NamesJSON()), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["forces"][0] != "Iron Jaw Binary" || got["female"][0] != "Ranna" {
		t.Errorf("names JSON: %v", got)
	}
	if all := strings.Join(c.Problems, "\n"); !strings.Contains(all, "Names.md") || !strings.Contains(all, "<bad>") {
		t.Errorf("an invalid name should be reported: %v", c.Problems)
	}

	write("Names.md", "---\nforce_namez: [Oops]\n---\n")
	c, _ = (Library{Root: root}).Campaign("test")
	if all := strings.Join(c.Problems, "\n"); !strings.Contains(all, "force_namez") {
		t.Errorf("an unknown key should be reported: %v", c.Problems)
	}
}

// Refusal War suggests Clan Wolf names, and its pages hand them to pilotnames.js.
func TestRefusalWarNamesReachThePages(t *testing.T) {
	c, ok := (Library{Root: "../../campaigns"}).Campaign("refusalwar")
	if !ok || len(c.Names.Forces) == 0 || len(c.Names.Male) == 0 || len(c.Names.Female) == 0 {
		t.Fatalf("Refusal War should have its own names: %+v", c.Names)
	}
	srv, _, cookie := startedCompany(t)
	for _, path := range []string{"/refusalwar/manage/"} {
		page := html.UnescapeString(body(t, srv, path, cookie))
		if !strings.Contains(page, `data-names="{"forces":["13th Wolf Guards"`) {
			t.Errorf("%s should carry the campaign's names for pilotnames.js", path)
		}
	}
}
