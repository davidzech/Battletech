package site

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/hangar"
	"github.com/davidzech/Battletech/internal/mekbay"
)

// Scenario 000 hands every cadet one of four standard machines instead of letting them bring their own.
var scenario000Pool = []string{"BMBlackHawk_Prime", "BMRyoken_Prime", "BMShadowCat_Prime", "BMPouncer_Prime", "BMGrendel_Prime"}

func TestProvidedForce(t *testing.T) {
	sc := &content.Scenario{ProvidedUnits: scenario000Pool}
	if !sc.Provides() || (&content.Scenario{}).Provides() || (*content.Scenario)(nil).Provides() {
		t.Fatal("provides() is true only for a scenario that lists units")
	}
	h := providedForce(sc)
	if len(h.Units) != 5 {
		t.Fatalf("units: %+v", h.Units)
	}
	ids := map[string]bool{}
	for i, u := range h.Units {
		cat, ok := catalog.ByName(u.Unit)
		if !ok || cat.Type != "Mek" {
			t.Errorf("%s is not a mech in the catalog", u.Unit)
		}
		if u.Unit != scenario000Pool[i] || !strings.HasPrefix(u.ID, providedPrefix) || ids[u.ID] || !hangar.Pristine(u) || u.Pilot != "" {
			t.Errorf("a provided unit is new, unique and uncrewed: %+v", u)
		}
		ids[u.ID] = true
	}
	// The views say what the popup needs: names, BV, no damage, and a condition of ok.
	views := hangarViews(h)
	if views[0].Name != "Nova" || views[1].Name != "Stormcrow" || views[2].Name != "Shadow Cat" || views[3].Name != "Pouncer" || views[4].Name != "Mongrel" || views[0].Condition != "ok" || views[0].BV != 2663 {
		t.Errorf("views: %+v", views[:1])
	}
}

// A mech listed twice is two mechs: each copy has its own ID, the first keeping provided-<name>, so both can be fielded together.
func TestProvidedForceRepeatsAMech(t *testing.T) {
	h := providedForce(&content.Scenario{ProvidedUnits: []string{"BMBlackHawk_Prime", "BMRyoken_Prime", "BMBlackHawk_Prime", "BMBlackHawk_Prime"}})
	var ids []string
	for _, u := range h.Units {
		ids = append(ids, u.ID)
	}
	want := []string{providedPrefix + "bmblackhawk_prime", providedPrefix + "bmryoken_prime", providedPrefix + "bmblackhawk_prime-2", providedPrefix + "bmblackhawk_prime-3"}
	if strings.Join(ids, " ") != strings.Join(want, " ") {
		t.Errorf("ids %v, want %v", ids, want)
	}
	if msg := deployProblem(h, []string{want[0], want[2]}, 0); msg != "" {
		t.Errorf("both Novas can be fielded: %q", msg)
	}
}

func TestProvidedScenarioDeploy(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	id := playthroughID("wolfpack2026")
	// A MekBay that can't be reached must not matter: the provided mechs are not read from it, and the hangar isn't touched.
	fake.setHangar(mekbay.HangarForce{}, errors.New("MekBay must not be read for a provided force"))
	const hangarPath = "/api/mekbay/hangar?campaign=refusalwar&scenario=scenario_000"
	r := get(t, srv, hangarPath, cookie)
	var view struct {
		Provided bool             `json:"provided"`
		Units    []hangarUnitView `json:"units"`
	}
	if r.StatusCode != http.StatusOK || json.NewDecoder(r.Body).Decode(&view) != nil {
		t.Fatalf("hangar json: %d", r.StatusCode)
	}
	if !view.Provided || len(view.Units) != 5 || view.Units[0].Name != "Nova" {
		t.Fatalf("the popup should list the five provided mechs: %+v", view)
	}
	// Scenario 001 still reads the hangar (and so fails here).
	if r := get(t, srv, "/api/mekbay/hangar?campaign=refusalwar&scenario=scenario_001", cookie); r.StatusCode != http.StatusBadGateway {
		t.Errorf("an ordinary scenario reads the hangar: %d", r.StatusCode)
	}
	// Nothing about the provided force is cached as the hangar.
	if h, _, ok, _ := store.CachedHangar(ctx, id, "refusalwar", goodInstance); ok {
		t.Errorf("a provided force must not be cached as the hangar: %+v", h)
	}
	// The page carries it at once, with the explanation.
	page := body(t, srv, "/refusalwar/scenario_000/", cookie)
	if !strings.Contains(page, "This mission provides its own mechs") || !strings.Contains(page, "&#34;provided&#34;:true") || !strings.Contains(page, "Pouncer") {
		t.Errorf("the scenario page should show the provided mechs and say so: %.600s", page)
	}
	if strings.Contains(body(t, srv, "/refusalwar/scenario_001/", cookie), "&#34;provided&#34;:true") {
		t.Error("Scenario 001 provides nothing")
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

func TestScenario000EnforcesItsRules(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, _ := newMekBayTestServer(t, fake)
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	pool := providedForce(&content.Scenario{ProvidedUnits: scenario000Pool})
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
		t.Error("Scenario 001 has no unit limit or pilot rule")
	}
}

// The variants a provided omnimech can be reconfigured to in Scenario 000 (provided_swaps): one Clan Wolf fields in the Clan Invasion era, and one it doesn't.
func swapChoices(t *testing.T, unit string) (fielded, not string) {
	t.Helper()
	cat, ok := catalog.ByName(unit)
	if !ok {
		t.Fatalf("%s is not in the catalog", unit)
	}
	available := catalog.Available("Clan Wolf", "Clan Invasion")
	for _, o := range hangar.SwapOptions(cat) {
		if available[o.Unit] && fielded == "" {
			fielded = o.Unit
		}
		if !available[o.Unit] && not == "" {
			not = o.Unit
		}
	}
	if fielded == "" || not == "" {
		t.Fatalf("%s needs a fielded and an unfielded variant for this test: %q %q", unit, fielded, not)
	}
	return fielded, not
}

// Scenario 000 lets the cadets reconfigure its provided omnimechs for free, to a variant Clan Wolf fields in the era; the battle is built with that variant.
func TestReconfigureProvidedMechs(t *testing.T) {
	c, _ := (content.Library{Root: "../../campaigns"}).Campaign("refusalwar")
	s0, _ := c.Scenario("scenario_000")
	s1, _ := c.Scenario("scenario_001")
	if !s0.ProvidedSwaps || s1.ProvidedSwaps {
		t.Fatalf("Scenario 000 allows free swaps, 001 doesn't: %v %v", s0.ProvidedSwaps, s1.ProvidedSwaps)
	}
	nova := providedPrefix + "bmblackhawk_prime"
	fielded, not := swapChoices(t, "BMBlackHawk_Prime")
	h := providedForce(s0)
	for name, tc := range map[string]struct {
		sc       *content.Scenario
		ids, tos []string
		problem  bool
		want     string
	}{
		"nothing chosen":           {s0, nil, nil, false, "BMBlackHawk_Prime"},
		"a fielded variant":        {s0, []string{nova}, []string{fielded}, false, fielded},
		"its own variant":          {s0, []string{nova}, []string{"BMBlackHawk_Prime"}, false, "BMBlackHawk_Prime"},
		"one Clan Wolf lacks":      {s0, []string{nova}, []string{not}, true, ""},
		"another chassis":          {s0, []string{nova}, []string{"BMRyoken_Prime"}, true, ""},
		"an unknown mech":          {s0, []string{"provided-nope"}, []string{fielded}, true, ""},
		"twice":                    {s0, []string{nova, nova}, []string{fielded, fielded}, true, ""},
		"unpaired":                 {s0, []string{nova}, nil, true, ""},
		"a scenario without swaps": {s1, []string{nova}, []string{fielded}, true, ""},
	} {
		got, msg := reconfigureProvided(h, c, tc.sc, tc.ids, tc.tos)
		if (msg != "") != tc.problem {
			t.Errorf("%s: problem %q", name, msg)
			continue
		}
		if tc.problem {
			continue
		}
		if got.Units[0].Unit != tc.want || got.Units[0].ID != nova {
			t.Errorf("%s: the Nova is %+v", name, got.Units[0])
		}
	}
	if h.Units[0].Unit != "BMBlackHawk_Prime" {
		t.Error("reconfiguring leaves the scenario's own force alone")
	}
	// The deploy data offers the swaps, with each variant marked as fielded or not.
	data := deployData(h, c, s0, false)
	if data["swaps"] != true || data["provided"] != true {
		t.Errorf("the deploy data offers the swaps: %v %v", data["swaps"], data["provided"])
	}
	marked := map[string]bool{}
	for _, o := range data["units"].([]hangarUnitView)[0].SwapOptions {
		marked[o.Unit] = o.Available
	}
	if !marked[fielded] || marked[not] {
		t.Errorf("the variants are marked as fielded or not: %v", marked)
	}
}
