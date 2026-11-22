package content

import (
	"os"
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
