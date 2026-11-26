package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/catalog"
)

func TestLookupUnitAndParseMul(t *testing.T) {
	for _, c := range []struct{ chassis, model, name string }{
		{"Koshi (Mist Lynx)", "E", "BMKoshi_E"},
		{"Grizzly", "", "BMGrizzly"},
		{"Mad Cat (Timber Wolf)", "H", "BMMadCat_H"},
	} {
		if u, ok := catalog.Lookup(c.chassis, c.model); !ok || u.Name != c.name || u.BV <= 0 || u.Tons <= 0 || u.Class == "" {
			t.Errorf("lookupUnit(%q, %q) = %+v, %v; want %s", c.chassis, c.model, u, ok, c.name)
		}
	}
	if u, ok := catalog.ByName("BMVulture_Prime"); !ok || u.BV != 2351 || catalog.DisplayName(u) != "Mad Dog Prime" {
		t.Errorf("unitByName(BMVulture_Prime) = %+v, %v, display %q", u, ok, catalog.DisplayName(u))
	}
	if _, ok := catalog.Lookup("No Such Mech", "X"); ok {
		t.Error("unknown unit should not be found")
	}

	raw, err := os.ReadFile("../../campaigns/RefusalWar/Scenario_000/OpFor1.mul")
	if err != nil {
		t.Fatal(err)
	}
	es, err := ParseMul(raw)
	if err != nil || len(es) != 3 {
		t.Fatalf("parseMul = %v, %v", es, err)
	}
	if es[0].Chassis != "Koshi (Mist Lynx)" || es[0].Model != "E" || es[0].Pilot != "Tyra" || es[0].Gunnery != 3 || es[0].Piloting != 4 {
		t.Errorf("first entity = %+v", es[0])
	}
	if _, err := ParseMul([]byte("not xml")); err == nil {
		t.Error("garbage should not parse")
	}
}

// A unit with no pilot, or a pilot without both skills, is an error rather than an elite 0/0 pilot; skills written out as 0 are taken as given.
func TestParseMulNeedsPilotSkills(t *testing.T) {
	for name, entity := range map[string]string{
		"no pilot":    `<entity chassis="Grizzly" model=""></entity>`,
		"no skills":   `<entity chassis="Grizzly" model=""><pilot name="Tyra"/></entity>`,
		"no piloting": `<entity chassis="Grizzly" model=""><pilot name="Tyra" gunnery="3"/></entity>`,
		"no gunnery":  `<entity chassis="Grizzly" model=""><pilot name="Tyra" piloting="4"/></entity>`,
	} {
		if es, err := ParseMul([]byte(`<unit><entity chassis="Koshi (Mist Lynx)" model="E"><pilot gunnery="3" piloting="4"/></entity>` + entity + `</unit>`)); err == nil || !strings.Contains(err.Error(), "unit 2 (Grizzly)") {
			t.Errorf("%s: %+v, %v", name, es, err)
		}
	}
	es, err := ParseMul([]byte(`<unit><entity chassis="Grizzly" model=""><pilot gunnery="0" piloting="0"/></entity></unit>`))
	if err != nil || len(es) != 1 || es[0].Gunnery != 0 || es[0].Piloting != 0 || es[0].Pilot != "" {
		t.Errorf("an explicit 0/0 pilot: %+v, %v", es, err)
	}
}

// A scenario's force files are checked when the campaign loads: an entry that isn't a file in the scenario's folder, a missing file, one that doesn't
// parse, a unit with no pilot skills and a unit the catalog doesn't know are all campaign problems; a good file is none.
func TestForceFilesAreCheckedAtLoad(t *testing.T) {
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
	write("Scenario_000/Setup.md", "---\nid: scenario_000\ntitle: First\nforces:\n  opfor1: Good.mul\n  opfor2: ../Outside.mul\n  opfor3: Missing.mul\n  opfor4: Broken.mul\n  opfor5: NoPilot.mul\n  opfor6: Unknown.mul\n---\n# First\n")
	write("Scenario_000/Good.mul", `<unit><entity chassis="Grizzly" model=""><pilot name="Tyra" gunnery="3" piloting="4"/></entity></unit>`)
	write("Scenario_000/Broken.mul", "not xml")
	write("Scenario_000/NoPilot.mul", `<unit><entity chassis="Grizzly" model=""></entity></unit>`)
	write("Scenario_000/Unknown.mul", `<unit><entity chassis="No Such Mech" model="X"><pilot gunnery="4" piloting="5"/></entity></unit>`)
	c, ok := (Library{Root: root}).Campaign("test")
	if !ok {
		t.Fatal("the campaign still loads")
	}
	all := strings.Join(c.Problems, "\n")
	for _, want := range []string{`opfor2: "../Outside.mul" must be a file`, "Missing.mul can't be read", "Broken.mul: read .mul", "NoPilot.mul: read .mul: unit 1 (Grizzly) needs a <pilot>", "Unknown.mul: No Such Mech X is not in the unit catalog"} {
		if !strings.Contains(all, "scenario_000/Setup.md: forces: "+want) {
			t.Errorf("the problems should mention %q:\n%s", want, all)
		}
	}
	if strings.Contains(all, "Good.mul") || len(c.Problems) != 5 {
		t.Errorf("only the five bad entries are problems:\n%s", all)
	}
}
