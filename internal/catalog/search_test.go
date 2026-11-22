package catalog

import (
	"strings"
	"testing"
)

func names(us []Unit) []string {
	out := make([]string, len(us))
	for i, u := range us {
		out[i] = u.Name
	}
	return out
}

// unavailableMech is a mech Clan Wolf does not field in the Clan Invasion era, with a query that finds it.
func unavailableMech(t *testing.T) (Unit, string) {
	t.Helper()
	wolf := Available("Clan Wolf", "Clan Invasion")
	buildSearch()
	for _, e := range searchable {
		if e.unit.TechBase == "Inner Sphere" && !wolf[e.unit.Name] && e.unit.Model != "" {
			return e.unit, e.unit.Chassis + " " + e.unit.Model
		}
	}
	t.Fatal("no unavailable mech found")
	return Unit{}, ""
}

func contains(us []Unit, name string) bool {
	for _, u := range us {
		if u.Name == name {
			return true
		}
	}
	return false
}

func TestSearchUnits(t *testing.T) {
	if got, n := Search("", nil); got != nil || n != 0 {
		t.Errorf("an empty query finds nothing: %v %d", got, n)
	}
	if got, _ := Search("  ?! ", nil); got != nil {
		t.Errorf("a query with no letters or digits finds nothing: %v", got)
	}
	// The Clan name and the Inner Sphere name both find it; case and punctuation don't matter.
	for _, q := range []string{"timber wolf", "TIMBER-WOLF", "madcat prime", "mad cat", "timberwolf"} {
		got, _ := Search(q, nil)
		found := false
		for _, u := range got {
			found = found || u.Chassis == "Mad Cat (Timber Wolf)"
		}
		if !found {
			t.Errorf("%q should find the Timber Wolf: %v", q, names(got))
		}
	}
	// Every typed word has to match, and only mechs are listed.
	got, total := Search("timber wolf prime", nil)
	for _, u := range got {
		if u.Type != "Mek" || !strings.Contains(collapse(u.Chassis+u.Model), "prime") {
			t.Errorf("unexpected hit for 'timber wolf prime': %+v", u)
		}
	}
	if total == 0 {
		t.Error("expected the Timber Wolf Prime")
	}
	if got, _ := Search("zzzzqqqq", nil); len(got) != 0 {
		t.Errorf("nonsense matched: %v", names(got))
	}
	for _, u := range func() []Unit { g, _ := Search("a", nil); return g }() {
		if u.Type != "Mek" {
			t.Errorf("%s is not a mech", u.Name)
		}
	}
	// A word that starts a name ranks before one found only inside a word.
	got, _ = Search("wolf", nil)
	if len(got) == 0 {
		t.Fatal("no wolves")
	}
	// The results are capped, and the total says how many matched.
	got, total = Search("a", nil)
	if len(got) != maxSearchResults || total <= maxSearchResults {
		t.Errorf("cap: %d shown of %d", len(got), total)
	}
}

func TestAvailabilityFilter(t *testing.T) {
	wolf := Available("Clan Wolf", "Clan Invasion")
	if len(wolf) < 400 {
		t.Fatalf("Clan Wolf should field hundreds of units in the Clan Invasion era, got %d", len(wolf))
	}
	if !wolf["BMMadCat_H"] || !wolf["BMVulture_Prime"] {
		t.Error("the Timber Wolf H and the Mad Dog Prime are Clan Wolf units in the Clan Invasion era")
	}
	if Available("Clan Wolf", "No Such Era") != nil || Available("No Such Clan", "Clan Invasion") != nil {
		t.Error("an unknown pair has no availability data, so no filter")
	}
	// A mech Clan Wolf doesn't field is found only when the filter is off.
	u, q := unavailableMech(t)
	if got, _ := Search(q, wolf); contains(got, u.Name) {
		t.Errorf("the filtered search offered %s", u.Name)
	}
	if got, _ := Search(q, nil); !contains(got, u.Name) {
		t.Errorf("the unfiltered search should find %s with %q", u.Name, q)
	}
	// Every Clan Wolf unit named in availability.json exists in the catalog.
	for n := range wolf {
		if _, ok := ByName(n); !ok {
			t.Errorf("availability names %s, which is not in the catalog", n)
		}
	}
}
