package site

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Scenario 000 hands every cadet one of four standard machines instead of letting them bring their own.
var scenario000Pool = []string{"BMVulture_Prime", "BMMadCat_Prime", "BMBlackHawk_Prime", "BMRyoken_Prime"}

func TestProvidedForce(t *testing.T) {
	sc := &Scenario{ProvidedUnits: scenario000Pool}
	if !sc.provides() || (&Scenario{}).provides() || (*Scenario)(nil).provides() {
		t.Fatal("provides() is true only for a scenario that lists units")
	}
	h := providedForce(sc)
	if len(h.Units) != 4 {
		t.Fatalf("units: %+v", h.Units)
	}
	ids := map[string]bool{}
	for i, u := range h.Units {
		cat, ok := unitByName(u.Unit)
		if !ok || cat.Type != "Mek" {
			t.Errorf("%s is not a mech in the catalog", u.Unit)
		}
		if u.Unit != scenario000Pool[i] || !strings.HasPrefix(u.ID, providedPrefix) || ids[u.ID] || !unitPristine(u) || u.Pilot != "" {
			t.Errorf("a provided unit is new, unique and uncrewed: %+v", u)
		}
		ids[u.ID] = true
	}
	// The views say what the popup needs: names, BV, no damage, and a condition of ok.
	views := hangarViews(h)
	if views[0].Name != "Mad Dog" || views[1].Name != "Timber Wolf" || views[2].Name != "Nova" || views[3].Name != "Stormcrow" || views[0].Condition != "ok" || views[0].BV != 2351 {
		t.Errorf("views: %+v", views[:1])
	}
}

// Scenario 000's own front matter lists the four mechs, and every provided unit of every scenario is a real mech.
func TestScenario000ProvidesItsOwnMechs(t *testing.T) {
	lib := Library{Root: "../../campaigns"}
	c, _ := lib.Campaign("refusalwar")
	sc, ok := c.Scenario("scenario_000")
	if !ok || strings.Join(sc.ProvidedUnits, ",") != strings.Join(scenario000Pool, ",") {
		t.Fatalf("Scenario 000 should provide Mad Dog, Timber Wolf, Nova and Stormcrow Primes: %+v", sc.ProvidedUnits)
	}
	if sc1, _ := c.Scenario("scenario_001"); sc1.provides() {
		t.Error("Sortie 001 uses the player's hangar")
	}
	camps, err := lib.Campaigns()
	if err != nil {
		t.Fatal(err)
	}
	for _, camp := range camps {
		for _, s := range camp.Scenarios {
			for _, name := range s.ProvidedUnits {
				if u, ok := unitByName(name); !ok || u.Type != "Mek" {
					t.Errorf("%s provides %q, which is not a mech in the catalog", s.Slug, name)
				}
			}
		}
	}
}

func TestProvidedScenarioDeploy(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}, "uid_p1": {uidP1}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	id := playthroughID("wolfpack2026")
	// A MekBay that can't be reached must not matter: the provided mechs are not read from it, and the hangar isn't touched.
	fake.setHangar(HangarForce{}, errors.New("MekBay must not be read for a provided force"))
	const hangarPath = "/api/mekbay/hangar?campaign=refusalwar&scenario=scenario_000"
	r := get(t, srv, hangarPath, cookie)
	var view struct {
		Provided bool             `json:"provided"`
		Units    []hangarUnitView `json:"units"`
	}
	if r.StatusCode != http.StatusOK || json.NewDecoder(r.Body).Decode(&view) != nil {
		t.Fatalf("hangar json: %d", r.StatusCode)
	}
	if !view.Provided || len(view.Units) != 4 || view.Units[0].Name != "Mad Dog" {
		t.Fatalf("the popup should list the four provided mechs: %+v", view)
	}
	// Sortie 001 still reads the hangar (and so fails here).
	if r := get(t, srv, "/api/mekbay/hangar?campaign=refusalwar&scenario=scenario_001", cookie); r.StatusCode != http.StatusBadGateway {
		t.Errorf("an ordinary scenario reads the hangar: %d", r.StatusCode)
	}
	// Nothing about the provided force is cached as the hangar.
	if h, _, ok, _ := store.CachedHangar(ctx, id, "refusalwar", goodInstance); ok {
		t.Errorf("a provided force must not be cached as the hangar: %+v", h)
	}
	// The page carries it at once, with the explanation.
	page := body(t, srv, "/refusalwar/scenario_000/", cookie)
	if !strings.Contains(page, "This mission provides its own mechs") || !strings.Contains(page, "&#34;provided&#34;:true") || !strings.Contains(page, "Mad Dog") {
		t.Errorf("the scenario page should show the provided mechs and say so: %.600s", page)
	}
	if strings.Contains(body(t, srv, "/refusalwar/scenario_001/", cookie), "&#34;provided&#34;:true") {
		t.Error("Sortie 001 provides nothing")
	}
	// The plan is checked against the provided mechs: they can be picked (with named pilots), the hangar's units can't.
	plan := func(kv ...string) *http.Response {
		v := url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_000"}, "players": {"1"}}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Add(kv[i], kv[i+1])
		}
		return post(t, srv, "/api/deploy/plan", v, cookie, srv.URL)
	}
	pick := view.Units[1].ID
	if got := plan("p1_units", pick, "p1_assign_unit", pick, "p1_assign_pilot", "Alrek").StatusCode; got != http.StatusOK { // Scenario 000 needs a named pilot on each mech
		t.Errorf("planning with a provided mech: %d", got)
	}
	if got := plan("p1_units", "ua").StatusCode; got != http.StatusBadRequest {
		t.Errorf("a hangar mech isn't available in a provided scenario: %d", got)
	}
	if got := plan("p1_units", pick, "p1_units", pick).StatusCode; got != http.StatusBadRequest {
		t.Errorf("a provided mech can only be taken once: %d", got)
	}
}

// Scenario 000 allows two mechs in all, each with a named pilot, and has no BV limit.
func TestScenarioRules(t *testing.T) {
	sc := &Scenario{UnitLimit: 2, PilotsRequired: true}
	crew := func(units ...string) []CrewAssignment {
		var out []CrewAssignment
		for _, u := range units {
			out = append(out, CrewAssignment{UnitID: u, Name: "Alrek"})
		}
		return out
	}
	for name, c := range map[string]struct {
		units [][]string
		crews [][]CrewAssignment
		bad   bool
	}{
		"two mechs, both crewed":      {[][]string{{"a", "b"}}, [][]CrewAssignment{crew("a", "b")}, false},
		"one each for two players":    {[][]string{{"a"}, {"b"}}, [][]CrewAssignment{crew("a"), crew("b")}, false},
		"three mechs is over":         {[][]string{{"a", "b"}, {"c"}}, [][]CrewAssignment{crew("a", "b"), crew("c")}, true},
		"a mech without a pilot":      {[][]string{{"a", "b"}}, [][]CrewAssignment{crew("a")}, true},
		"a pilot sent empty":          {[][]string{{"a"}}, [][]CrewAssignment{{{UnitID: "a"}}}, true},
		"no assignments at all":       {[][]string{{"a"}}, nil, true},
		"another player's pilot only": {[][]string{{"a"}, {"b"}}, [][]CrewAssignment{crew("b"), crew("b")}, true},
	} {
		if msg := sc.rulesProblem(c.units, c.crews); (msg != "") != c.bad {
			t.Errorf("%s: %q", name, msg)
		}
	}
	// No rules, no problems.
	if msg := (&Scenario{}).rulesProblem([][]string{{"a", "b", "c"}}, nil); msg != "" {
		t.Errorf("a scenario without rules: %q", msg)
	}
}

func TestScenario000EnforcesItsRules(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, _ := newMekBayTestServer(t, fake)
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}, "uid_p1": {uidP1}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	pool := providedForce(&Scenario{ProvidedUnits: scenario000Pool})
	id := func(i int) string { return pool.Units[i].ID }
	plan := func(kv ...string) int {
		v := url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_000"}, "players": {"1"}}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Add(kv[i], kv[i+1])
		}
		return post(t, srv, "/api/deploy/plan", v, cookie, srv.URL).StatusCode
	}
	if got := plan("p1_units", id(0), "p1_units", id(1), "p1_assign_unit", id(0), "p1_assign_pilot", "Alrek", "p1_assign_unit", id(1), "p1_assign_pilot", "Katarina"); got != http.StatusOK {
		t.Errorf("two crewed mechs: %d", got)
	}
	if got := plan("p1_units", id(0), "p1_units", id(1), "p1_units", id(2), "p1_assign_unit", id(0), "p1_assign_pilot", "Alrek"); got != http.StatusBadRequest {
		t.Errorf("three mechs: %d", got)
	}
	if got := plan("p1_units", id(0), "p1_units", id(1), "p1_assign_unit", id(0), "p1_assign_pilot", "Alrek"); got != http.StatusBadRequest {
		t.Errorf("a mech without a pilot: %d", got)
	}
	if got := plan("p1_units", id(0)); got != http.StatusBadRequest {
		t.Errorf("no pilot at all: %d", got)
	}
	page := body(t, srv, "/refusalwar/scenario_000/", cookie)
	if !strings.Contains(page, `data-unit-limit="2"`) || !strings.Contains(page, `data-pilots-required="1"`) || !strings.Contains(page, `data-limit="0"`) {
		t.Errorf("the popup is told the rules: %.400s", page)
	}
	if !strings.Contains(page, "This mission provides its own mechs. They are only used for this mission, and will be lost upon completion.") {
		t.Error("the short provided-mechs text")
	}
	if p := body(t, srv, "/refusalwar/scenario_001/", cookie); !strings.Contains(p, `data-unit-limit="0"`) || strings.Contains(p, `data-pilots-required="1"`) {
		t.Error("Sortie 001 has no unit limit or pilot rule")
	}
}
