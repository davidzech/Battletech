package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Temper Edge's allies load from Allies.mul; an allies entry that isn't a readable file of known units is a campaign problem.
func TestAllies(t *testing.T) {
	sc, _ := refusalWar(t).Scenario("scenario_004")
	if len(sc.AlliedUnits) != 2 || sc.AlliedUnits[0].Name != "Mist Lynx C" || sc.AlliedUnits[0].Gunnery != 3 || sc.AlliedUnits[0].Piloting != 2 || sc.AlliedUnits[1].Name != "Savior Repair Vehicle" {
		t.Errorf("Temper Edge's allies: %+v", sc.AlliedUnits)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Bad.mul"), []byte(`<unit><entity chassis="Nothing" model="X"><pilot gunnery="4" piloting="5"/></entity></unit>`), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := &Scenario{Dir: dir, Allies: []string{"Missing.mul", "../Up.mul", "Bad.mul"}}
	problems := strings.Join(bad.loadAllies(), "\n")
	for _, want := range []string{"Missing.mul can't be read", `"../Up.mul" must be a file in the scenario's folder`, "Nothing X is not in the unit catalog"} {
		if !strings.Contains(problems, want) {
			t.Errorf("missing problem %q in:\n%s", want, problems)
		}
	}
}

// A readout is a planetary survey unless the scenario says otherwise: the missions on moons (MacDuff, Karetah) are lunar surveys.
func TestReadoutTitles(t *testing.T) {
	c := refusalWar(t)
	for id, want := range map[string]string{"scenario_004": "Planetary survey", "scenario_005": "Lunar survey", "scenario_010": "Lunar survey"} {
		if sc, _ := c.Scenario(id); sc.ReadoutTitle != want {
			t.Errorf("%s: %q, want %q", id, sc.ReadoutTitle, want)
		}
	}
}
