package site

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const ownerUID = "e66a3544-8fcf-4dab-955a-f4ea2db0b610"

func TestLookupUnitAndParseMul(t *testing.T) {
	for _, c := range []struct{ chassis, model, name string }{
		{"Koshi (Mist Lynx)", "E", "BMKoshi_E"},
		{"Grizzly", "", "BMGrizzly"},
		{"Mad Cat (Timber Wolf)", "H", "BMMadCat_H"},
	} {
		if u, ok := lookupUnit(c.chassis, c.model); !ok || u.Name != c.name || u.BV <= 0 || u.Tons <= 0 || u.Class == "" {
			t.Errorf("lookupUnit(%q, %q) = %+v, %v; want %s", c.chassis, c.model, u, ok, c.name)
		}
	}
	if u, ok := unitByName("BMVulture_Prime"); !ok || u.BV != 2351 || displayName(u) != "Mad Dog Prime" {
		t.Errorf("unitByName(BMVulture_Prime) = %+v, %v, display %q", u, ok, displayName(u))
	}
	if _, ok := lookupUnit("No Such Mech", "X"); ok {
		t.Error("unknown unit should not be found")
	}

	raw, err := os.ReadFile("../../campaigns/RefusalWar/Scenario_000/OpFor1.mul")
	if err != nil {
		t.Fatal(err)
	}
	es, err := parseMul(raw)
	if err != nil || len(es) != 3 {
		t.Fatalf("parseMul = %v, %v", es, err)
	}
	if es[0].Chassis != "Koshi (Mist Lynx)" || es[0].Model != "E" || es[0].Pilot != "Tyra" || es[0].Gunnery != 3 || es[0].Piloting != 4 {
		t.Errorf("first entity = %+v", es[0])
	}
	if _, err := parseMul([]byte("not xml")); err == nil {
		t.Error("garbage should not parse")
	}
}

func TestAdjustedBV(t *testing.T) {
	for _, c := range []struct{ base, g, p, want int }{
		{1000, 4, 5, 1000}, // the standard pilot
		{1000, 3, 4, 1320},
		{1000, 0, 0, 2420},
		{1000, 9, 9, 640}, // skills past 8 count as 8
		{2351, 4, 5, 2351},
	} {
		if got := adjustedBV(c.base, c.g, c.p); got != c.want {
			t.Errorf("adjustedBV(%d, %d, %d) = %d, want %d", c.base, c.g, c.p, got, c.want)
		}
	}
}

func TestEveryScenarioForceResolves(t *testing.T) {
	lib := Library{Root: "../../campaigns"}
	cs, err := lib.Campaigns()
	if err != nil {
		t.Fatal(err)
	}
	for i := range cs {
		for j := range cs[i].Scenarios {
			sc := &cs[i].Scenarios[j]
			if len(sc.Forces) == 0 {
				continue
			}
			d, err := opForDraft(cs[i], sc)
			if err != nil {
				t.Errorf("%s/%s: %v", cs[i].Slug, sc.Slug, err)
				continue
			}
			if len(d.Groups) != len(sc.Forces) {
				t.Errorf("%s: %d groups for %d force files", sc.Slug, len(d.Groups), len(sc.Forces))
			}
		}
	}
}

func TestScenarioBVLimit(t *testing.T) {
	c, ok := (Library{Root: "../../campaigns"}).Campaign("refusalwar")
	if !ok {
		t.Fatal("no campaign")
	}
	if sc, _ := c.Scenario("scenario_001"); sc.BVLimit != 10000 {
		t.Errorf("scenario 001 BV limit = %d, want 10000", sc.BVLimit)
	}
	if sc, _ := c.Scenario("scenario_000"); sc.BVLimit != 0 {
		t.Errorf("scenario 000 should have no BV limit, got %d", sc.BVLimit)
	}
}

func TestBuildForceIsPristine(t *testing.T) {
	d := ForceDraft{Name: "X - OpFor", Groups: []GroupDraft{{Name: "OpFor 1", Units: []UnitDraft{{Unit: "BMGrizzly", BV: 1500, Pilot: "Drusus", Gunnery: 3, Piloting: 4}}}, {Name: "OpFor 2", Units: []UnitDraft{{Unit: "BMLoki_Prime", BV: 2000}}}}}
	f := buildForce("force-id", time.Date(2026, 11, 16, 7, 4, 11, 0, time.UTC), d)
	b, _ := json.Marshal(f)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["instanceId"] != "force-id" || m["type"] != "cbt" || m["bv"] != float64(3500) || m["timestamp"] != "2026-11-16T07:04:11.000Z" {
		t.Errorf("force header = %s", b)
	}
	if len(f.Groups) != 2 || f.Groups[0].Name != "OpFor 1" || f.Groups[0].Units[0].Unit != "BMGrizzly" {
		t.Fatalf("groups = %+v", f.Groups)
	}
	st := f.Groups[0].Units[0].State
	if st.Modified || st.Destroyed || len(st.Locations) != 0 || st.Crew[0].Name != "Drusus" || st.Crew[0].Gunnery != 3 {
		t.Errorf("unit state = %+v", st)
	}
	if f.Groups[0].ID == f.Groups[1].ID || f.Groups[0].Units[0].ID == f.Groups[1].Units[0].ID {
		t.Error("groups and units need distinct IDs")
	}
}

func decodeForce(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// groupSummary lists "name:unit,unit" for each group of a force, to compare layouts at a glance.
func groupSummary(force map[string]any) []string {
	var out []string
	for _, g := range force["groups"].([]any) {
		gm := g.(map[string]any)
		var ids []string
		for _, u := range gm["units"].([]any) {
			ids = append(ids, u.(map[string]any)["id"].(string))
		}
		name, _ := gm["name"].(string)
		out = append(out, name+":"+strings.Join(ids, ","))
	}
	return out
}

func TestDeployGroups(t *testing.T) {
	const sample = `{"instanceId":"f1","timestamp":"t0","c3Networks":[{"id":"n"}],"groups":[
		{"id":"g-a","name":"Alpha","color":"red","formationTargetGroupId":"g-dep","units":[{"id":"u1","state":{"locations":{"HD":{"armor":3}}}},{"id":"u2"}]},
		{"id":"g-dep","name":" deployed ","color":"blue","formationTargetGroupId":"g-a","units":[{"id":"u3"}]},
		{"id":"g-b","units":[{"id":"u4"}]}]}`
	n := 0
	newID := func() string { n++; return "new" + string(rune('0'+n)) }

	f := decodeForce(t, sample)
	if err := deployGroups(f, map[string]bool{"u2": true, "u3": true}, newID); err != nil {
		t.Fatal(err)
	}
	// "deployed" (any case, padded) is reused with its ID and color; the rest become a fresh Hangar group.
	want := []string{" deployed :u2,u3", "Hangar:u1,u4"}
	if got := groupSummary(f); !reflect.DeepEqual(got, want) {
		t.Fatalf("groups = %q, want %q", got, want)
	}
	groups := f["groups"].([]any)
	dep, hang := groups[0].(map[string]any), groups[1].(map[string]any)
	if dep["id"] != "g-dep" || dep["color"] != "blue" || hang["id"] != "new1" {
		t.Errorf("deployed = %v, hangar = %v", dep, hang)
	}
	// Alpha was dissolved, so Deployed's formation link to it is dropped.
	if _, has := dep["formationTargetGroupId"]; has {
		t.Error("a link to a dissolved group should be removed")
	}
	// Units keep their state, and the rest of the force is untouched.
	u1 := hang["units"].([]any)[0].(map[string]any)
	if u1["state"].(map[string]any)["locations"].(map[string]any)["HD"].(map[string]any)["armor"] != float64(3) {
		t.Errorf("unit state lost: %v", u1)
	}
	if f["c3Networks"] == nil || f["timestamp"] != "t0" {
		t.Errorf("unrelated fields changed: %v", f)
	}

	// Deploying everything leaves no Hangar group; deploying nothing leaves no Deployed group.
	f = decodeForce(t, sample)
	if err := deployGroups(f, map[string]bool{"u1": true, "u2": true, "u3": true, "u4": true}, newID); err != nil || len(groupSummary(f)) != 1 {
		t.Errorf("all deployed: %v, %v", groupSummary(f), err)
	}
	f = decodeForce(t, sample)
	if err := deployGroups(f, map[string]bool{}, newID); err != nil || len(groupSummary(f)) != 1 || !strings.HasPrefix(groupSummary(f)[0], "Hangar:") {
		t.Errorf("none deployed: %v, %v", groupSummary(f), err)
	}
	if err := deployGroups(decodeForce(t, sample), map[string]bool{"nope": true}, newID); err == nil {
		t.Error("an unknown unit should be refused")
	}
}

func hangarFixture() HangarForce {
	return HangarForce{Name: "Test Hangar", Timestamp: "t", Units: []HangarUnit{
		{ID: "u-vulture", Unit: "BMVulture_Prime", Group: "Hangar", Pilot: "Anya", Gunnery: 3, Piloting: 4}, // a skilled pilot: still base BV
		{ID: "u-loki1", Unit: "BMLoki_Prime", Group: "Deployed", Gunnery: 4, Piloting: 5, Damaged: true},
		{ID: "u-loki2", Unit: "BMLoki_Prime", Group: "Hangar", Gunnery: 4, Piloting: 5},
		{ID: "u-loki3", Unit: "BMLoki_Prime", Group: "Hangar", Gunnery: 4, Piloting: 5},
		{ID: "u-loki4", Unit: "BMLoki_Prime", Group: "Hangar", Gunnery: 4, Piloting: 5},
		{ID: "u-loki5", Unit: "BMLoki_Prime", Group: "Hangar", Gunnery: 4, Piloting: 5},
		{ID: "u-dead", Unit: "BMGrizzly", Group: "Hangar", Gunnery: 4, Piloting: 5, Destroyed: true},
		{ID: "u-odd", Unit: "BMNoSuchMech_X", Group: "Hangar", Gunnery: 4, Piloting: 5},
	}}
}

func TestHangarViewsAndDeployProblem(t *testing.T) {
	h := hangarFixture()
	views := hangarViews(h)
	if v := views[0]; v.Name != "Mad Dog" || v.Model != "Prime" || v.Icon != "/icons/vulture.png" || v.BV != 2351 || v.Pilot != "Anya" || v.Class != "Heavy" || v.Tons != 60 || v.InDeployed {
		t.Errorf("vulture view = %+v", v)
	}
	if v := views[1]; !v.InDeployed || !v.Damaged || v.BV <= 0 {
		t.Errorf("loki view = %+v", v)
	}
	if v := views[7]; !v.Unknown || v.Name != "BMNoSuchMech_X" {
		t.Errorf("unknown view = %+v", v)
	}
	for _, c := range []struct {
		ids   []string
		limit int
		want  string // substring of the problem, or "" for fine
	}{
		{[]string{"u-vulture", "u-loki1"}, 10000, ""},
		{[]string{"u-vulture", "u-loki1"}, 0, ""},
		{nil, 10000, "at least one"},
		{[]string{"u-vulture", "u-vulture"}, 0, "twice"},
		{[]string{"u-ghost"}, 0, "no longer in your hangar"},
		{[]string{"u-dead"}, 0, "destroyed"},
		{[]string{"u-odd"}, 0, "unit catalog"},
		{[]string{"u-loki1", "u-loki2", "u-loki3", "u-loki4", "u-loki5"}, 10000, "over this scenario's limit of 10000"},
		{[]string{"u-loki1", "u-loki2", "u-loki3", "u-loki4", "u-loki5"}, 0, ""},
	} {
		got := deployProblem(h, c.ids, c.limit)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("deployProblem(%v, %d) = %q, want it to contain %q", c.ids, c.limit, got, c.want)
		}
	}
}

func TestParseHangarForce(t *testing.T) {
	raw := json.RawMessage(`{"action":"forceData","data":{"name":"H","timestamp":"t1","groups":[
		{"name":"Deployed","units":[{"id":"a","unit":"BMGrizzly","state":{"locations":{"HD":{"armor":3}},"crew":[{"name":"Anya","gunnerySkill":3,"pilotingSkill":4}]}}]},
		{"name":"Hangar","units":[
			{"id":"b","unit":"BMLoki_Prime","state":{"crits":[{"id":"x","hits":1}],"crew":[{"name":"","gunnerySkill":4,"pilotingSkill":5}]}},
			{"id":"c","unit":"BMUller_J","state":{"destroyed":true,"locations":{}}},
			{"id":"d","unit":"BMUller_J","state":{"crits":[{"id":"y"}],"locations":{}}}]}]}}`)
	h, err := parseHangarForce(raw)
	if err != nil || h.Name != "H" || h.Timestamp != "t1" || len(h.Units) != 4 {
		t.Fatalf("parseHangarForce = %+v, %v", h, err)
	}
	a, b, c, d := h.Units[0], h.Units[1], h.Units[2], h.Units[3]
	if a.Group != "Deployed" || a.Pilot != "Anya" || a.Gunnery != 3 || a.Piloting != 4 || !a.Damaged || a.Destroyed {
		t.Errorf("a = %+v", a)
	}
	if !b.Damaged || b.Group != "Hangar" {
		t.Errorf("a crit hit should count as damage: %+v", b)
	}
	if !c.Destroyed || c.Damaged {
		t.Errorf("c = %+v", c)
	}
	if d.Damaged {
		t.Errorf("an untouched crit slot is not damage: %+v", d)
	}
	if _, err := parseHangarForce(json.RawMessage(`{"action":"forceData","data":null}`)); err == nil {
		t.Error("a missing force should be an error")
	}
}

// fakeMekBay serves a fixed hangar and records requests instead of calling mekbay.com.
type fakeMekBay struct {
	mu     sync.Mutex
	hangar HangarForce
	reqs   []OperationRequest
	err    error
	// services records repair and rearm requests; serviceErr makes them fail.
	services   []ServiceRequest
	purchases  []PurchaseRequest
	clones     []CloneRequest
	sales      []SellRequest
	swaps      []SwapRequest
	cloneErr   error
	serviceErr error
}

func (f *fakeMekBay) ReadHangar(context.Context, string) (HangarForce, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hangar, f.err
}

func (f *fakeMekBay) CloneHangar(_ context.Context, req CloneRequest) (HangarForce, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clones = append(f.clones, req)
	if f.cloneErr != nil {
		return HangarForce{}, f.cloneErr
	}
	return f.hangar, f.err
}

func (f *fakeMekBay) SellUnit(_ context.Context, req SellRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sales = append(f.sales, req)
	return f.serviceErr
}

func (f *fakeMekBay) SwapUnit(_ context.Context, req SwapRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.swaps = append(f.swaps, req)
	return f.serviceErr
}

func (f *fakeMekBay) ServiceUnit(_ context.Context, req ServiceRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.services = append(f.services, req)
	return f.serviceErr
}

func (f *fakeMekBay) PurchaseUnits(_ context.Context, req PurchaseRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.purchases = append(f.purchases, req)
	return f.serviceErr
}

func (f *fakeMekBay) SyncOperation(_ context.Context, req OperationRequest) (OperationResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	if f.err != nil {
		return OperationResult{}, f.err
	}
	res := OperationResult{OpForInstance: req.OpForInstance, OperationID: req.OperationID}
	if res.OpForInstance == "" {
		res.OpForInstance = "opfor-" + goodInstance[:8]
	}
	if res.OperationID == "" {
		res.OperationID = "op-" + goodInstance[:8]
	}
	return res, nil
}

func newMekBayTestServer(t *testing.T, mb MekBay) (*httptest.Server, *Store) {
	t.Helper()
	store, err := OpenSQLite(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	srv := httptest.NewServer(NewHandler("../../campaigns", store, WithMekBay(mb)))
	t.Cleanup(srv.Close)
	return srv, store
}

func get(t *testing.T, srv *httptest.Server, path string, cookie *http.Cookie) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func TestDeployFlow(t *testing.T) {
	fake := &fakeMekBay{hangar: hangarFixture()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	const (
		page   = "/refusalwar/scenario_001/"
		hangar = "/api/mekbay/hangar?campaign=refusalwar&scenario=scenario_001"
	)
	deploy := func(units []string, extra ...string) url.Values {
		v := url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_001"}, "units": units}
		for i := 0; i+1 < len(extra); i += 2 {
			v.Set(extra[i], extra[i+1])
		}
		return v
	}
	mine := []string{"u-vulture", "u-loki1"}

	if !strings.Contains(body(t, srv, page, nil), "Pyre Light") {
		t.Fatal("test page path is wrong: the scenario page did not load")
	}
	if strings.Contains(body(t, srv, page, nil), `id="mekbay-op"`) {
		t.Fatal("the deploy section must be hidden without a registered hangar")
	}

	// No seed, then a seed with no company, then a company whose campaign isn't started: all refused.
	if r := get(t, srv, hangar, nil); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("hangar without a seed: %d", r.StatusCode)
	}
	res := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "")
	cookie := res.Cookies()[0]
	id := playthroughID("wolfpack2026")
	if r := get(t, srv, hangar, cookie); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("hangar before naming the company: %d", r.StatusCode)
	}
	if r := post(t, srv, "/api/account", url.Values{"account": {"Wolf's Dragoons"}}, cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("company: %d", r.StatusCode)
	}
	if r := get(t, srv, hangar, cookie); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("hangar before starting the campaign: %d", r.StatusCode)
	}
	if r := post(t, srv, "/api/mekbay/deploy", deploy(mine), cookie, srv.URL); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("deploy before starting the campaign: %d", r.StatusCode)
	}
	if p := body(t, srv, page, cookie); strings.Contains(p, `id="mekbay-op"`) || !strings.Contains(p, "Start this campaign") {
		t.Fatalf("an unstarted campaign should point to its start button, not offer deploying: %.900s", p)
	}

	// A run from before the site kept its own copy of the hangar: the player can look and pick, but the site can't deploy for them.
	if err := store.StartRun(ctx, id, "refusalwar", Hangar{URL: goodHangarURL, Instance: goodInstance}); err != nil {
		t.Fatal(err)
	}

	// Naming pilots is a requirement for starting a campaign, not for deploying: pilots can die mid-campaign, so a company
	// with none can still deploy.
	if r := get(t, srv, hangar, cookie); r.StatusCode != http.StatusOK {
		t.Fatalf("hangar without named pilots: %d", r.StatusCode)
	}
	p := body(t, srv, page, cookie)
	if i, j := strings.Index(p, "/static/mechrow.js"), strings.Index(p, "/static/mekbay.js"); i < 0 || j < 0 || i > j {
		t.Fatal("the scenario page should load mechrow.js before mekbay.js")
	}
	if !strings.Contains(p, `id="deploy-open"`) || !strings.Contains(p, `data-min-players="1"`) || !strings.Contains(p, `data-max-players="2"`) || !strings.Contains(p, "up to 10000 BV") {
		t.Fatalf("read-only page: %.900s", p)
	}
	r := get(t, srv, hangar, cookie)
	var view struct {
		Name         string           `json:"name"`
		Limit        int              `json:"limit"`
		CanModify    bool             `json:"canModify"`
		HasOperation bool             `json:"hasOperation"`
		Units        []hangarUnitView `json:"units"`
	}
	if r.StatusCode != http.StatusOK || json.NewDecoder(r.Body).Decode(&view) != nil {
		t.Fatalf("hangar json: %d", r.StatusCode)
	}
	if view.Limit != 10000 || view.CanModify || view.HasOperation || len(view.Units) != 8 || view.Units[0].Name != "Mad Dog" || view.Units[0].BV != 2351 {
		t.Fatalf("hangar view = %+v", view)
	}
	if r := post(t, srv, "/api/mekbay/deploy", deploy(mine), cookie, srv.URL); r.StatusCode != http.StatusConflict {
		t.Fatalf("deploy without an owned hangar: %d", r.StatusCode)
	}

	// Modify mode: the page says so, and the server checks the choice before touching MekBay.
	if err := store.StartRun(ctx, id, "refusalwar", Hangar{URL: goodHangarURL, Instance: goodInstance, Source: goodHangarURL}); err != nil {
		t.Fatal(err)
	}
	if p := body(t, srv, page, cookie); strings.Contains(p, MekBayUID("wolfpack2026", SlotHangar)) {
		t.Fatal("the page must never show the site's MekBay account ID")
	}
	for name, c := range map[string]struct {
		form   url.Values
		origin string
		status int
	}{
		"cross-origin":    {deploy(mine), "https://evil.example", http.StatusForbidden},
		"nothing chosen":  {deploy(nil), srv.URL, http.StatusBadRequest},
		"unknown unit":    {deploy([]string{"u-ghost"}), srv.URL, http.StatusBadRequest},
		"destroyed unit":  {deploy([]string{"u-dead"}), srv.URL, http.StatusBadRequest},
		"over the limit":  {deploy([]string{"u-loki1", "u-loki2", "u-loki3", "u-loki4", "u-loki5"}), srv.URL, http.StatusBadRequest},
		"unknown mission": {deploy(mine, "scenario", "scenario_999"), srv.URL, http.StatusNotFound},
	} {
		if r := post(t, srv, "/api/mekbay/deploy", c.form, cookie, c.origin); r.StatusCode != c.status {
			t.Errorf("%s: got %d, want %d", name, r.StatusCode, c.status)
		}
	}
	if len(fake.reqs) != 0 {
		t.Fatalf("refused requests reached MekBay: %d calls", len(fake.reqs))
	}

	// A good choice reaches MekBay with the owner's UID, the chosen units and a fresh OpFor.
	r = post(t, srv, "/api/mekbay/deploy", deploy(mine), cookie, srv.URL)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("deploy: %d", r.StatusCode)
	}
	var out map[string]string
	_ = json.NewDecoder(r.Body).Decode(&out)
	if out["url"] != "https://mekbay.com/?operation=op-"+goodInstance[:8] {
		t.Fatalf("url = %q", out["url"])
	}
	if len(fake.reqs) != 1 {
		t.Fatalf("expected one MekBay call, got %d", len(fake.reqs))
	}
	req := fake.reqs[0]
	if req.OwnerUID != MekBayUID("wolfpack2026", SlotHangar) || req.CampaignInstance != goodInstance || req.OperationName != "Refusal War - Scenario 001" ||
		!reflect.DeepEqual(req.DeployedUnitIDs, mine) || req.OpForInstance != "" || req.KeepOpFor {
		t.Errorf("request = %+v", req)
	}
	if len(req.OpFor.Groups) != 2 || req.OpFor.Groups[0].Name != "OpFor 1" || req.OpFor.Groups[0].Units[0].BV <= 0 {
		t.Errorf("opfor groups = %+v", req.OpFor.Groups)
	}
	if op, ok, _ := store.MekBayOperation(ctx, id, "refusalwar", "scenario_001"); !ok || op.OperationID == "" || op.OpForInstance == "" {
		t.Fatalf("operation not remembered: %+v", op)
	}
	if p := body(t, srv, page, cookie); !strings.Contains(p, `href="https://mekbay.com/?operation=op-`+goodInstance[:8]+`"`) {
		t.Fatalf("page should link to the operation: %.900s", p)
	}
	r = get(t, srv, hangar, cookie)
	view.HasOperation = false
	if json.NewDecoder(r.Body).Decode(&view) != nil || !view.HasOperation || !view.CanModify {
		t.Fatalf("hangar view after deploying = %+v", view)
	}

	// Deploying again keeps the OpFor (and whatever damage it has) unless asked to reset it.
	post(t, srv, "/api/mekbay/deploy", deploy([]string{"u-vulture"}), cookie, srv.URL)
	again := fake.reqs[1]
	if !again.KeepOpFor || again.OpForInstance != "opfor-"+goodInstance[:8] || again.OperationID != "op-"+goodInstance[:8] {
		t.Fatalf("redeploy should reuse the OpFor and operation: %+v", again)
	}
	post(t, srv, "/api/mekbay/deploy", deploy(mine, "reset_opfor", "1"), cookie, srv.URL)
	if reset := fake.reqs[2]; reset.KeepOpFor || reset.OpForInstance != "opfor-"+goodInstance[:8] {
		t.Fatalf("reset should rewrite the same OpFor force: %+v", reset)
	}

	// MekBay failures reach the player.
	fake.err = errors.New("MekBay says that user ID doesn't own this hangar, so it can't be changed")
	if r := post(t, srv, "/api/mekbay/deploy", deploy(mine), cookie, srv.URL); r.StatusCode != http.StatusBadGateway {
		t.Fatalf("mekbay error: %d", r.StatusCode)
	}
	if r := get(t, srv, hangar, cookie); r.StatusCode != http.StatusBadGateway {
		t.Fatalf("mekbay read error: %d", r.StatusCode)
	}
}

// fakeServer is a stand-in for MekBay's WebSocket server holding one hangar force.
type fakeServer struct {
	mu      sync.Mutex
	actions []string
	saved   []map[string]any // every force saved, in order
	op      map[string]any
	uuids   []string
}

const (
	fakeOpForID = "opfor-existing"
	fakeHangar  = `{"instanceId":"` + goodInstance + `","timestamp":"2026-11-16T07:04:11.406Z","name":"Hangar","owned":false,"c3Networks":[],
		"groups":[{"id":"g1","name":"Alpha","units":[{"id":"u1","unit":"BMGrizzly","state":{"crew":[{"name":"A","gunnerySkill":4,"pilotingSkill":5}]}},{"id":"u2","unit":"BMLoki_Prime","state":{}}]}]}`
)

func (fs *fakeServer) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		reply := func(rid string, v map[string]any) {
			v["requestId"] = rid
			b, _ := json.Marshal(v)
			_ = c.Write(ctx, websocket.MessageText, b)
		}
		for {
			_, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			rid, _ := m["requestId"].(string)
			fs.mu.Lock()
			fs.actions = append(fs.actions, m["action"].(string))
			fs.mu.Unlock()
			if m["sessionId"] == nil || rid == "" {
				t.Errorf("message without session or request id: %s", b)
			}
			switch m["action"] {
			case "register":
				fs.mu.Lock()
				fs.uuids = append(fs.uuids, m["uuid"].(string))
				fs.mu.Unlock()
				if m["version"] != float64(2) {
					t.Errorf("register = %s", b)
				}
				reply("", map[string]any{"action": "userState"}) // unrelated pushes must be skipped
			case "getForce":
				switch m["instanceId"] {
				case goodInstance:
					reply(rid, map[string]any{"action": "forceData", "data": decodeForce(t, fakeHangar)})
				case fakeOpForID:
					reply(rid, map[string]any{"action": "forceData", "data": map[string]any{"timestamp": "2026-11-01T00:00:00.000Z"}})
				default:
					reply(rid, map[string]any{"action": "forceData", "data": nil})
				}
			case "saveForce":
				fs.mu.Lock()
				fs.saved = append(fs.saved, m["data"].(map[string]any))
				fs.mu.Unlock()
				reply(rid, map[string]any{"action": "forceSaved"})
			case "saveOperation": // no reply, like the real server
				fs.mu.Lock()
				fs.op = m["data"].(map[string]any)
				fs.mu.Unlock()
			case "getOperation":
				fs.mu.Lock()
				data := fs.op
				fs.mu.Unlock()
				reply(rid, map[string]any{"action": "operationData", "data": data})
			}
		}
	}
}

func TestMekBayClientProtocol(t *testing.T) {
	d := ForceDraft{Name: "N - OpFor", Groups: []GroupDraft{{Name: "OpFor 1", Units: []UnitDraft{{Unit: "BMGrizzly", BV: 5}}}}}

	t.Run("deploy and create", func(t *testing.T) {
		fs := &fakeServer{}
		ts := httptest.NewServer(fs.handler(t))
		defer ts.Close()
		cl := &MekBayClient{URL: "ws" + strings.TrimPrefix(ts.URL, "http")}
		res, err := cl.SyncOperation(context.Background(), OperationRequest{
			OwnerUID: ownerUID, CampaignInstance: goodInstance, DeployedUnitIDs: []string{"u2"}, OpFor: d, OperationName: "N",
		})
		if err != nil {
			t.Fatal(err)
		}
		fs.mu.Lock()
		defer fs.mu.Unlock()
		// The hangar is saved first (it proves ownership), then the OpFor, then the operation.
		if got := strings.Join(fs.actions, ","); got != "register,getForce,saveForce,saveForce,saveOperation,getOperation" {
			t.Fatalf("actions = %s", got)
		}
		if fs.uuids[0] != ownerUID {
			t.Errorf("registered as %q", fs.uuids[0])
		}
		hangar, opfor := fs.saved[0], fs.saved[1]
		if want := []string{"Deployed:u2", "Hangar:u1"}; !reflect.DeepEqual(groupSummary(hangar), want) {
			t.Errorf("hangar groups = %q, want %q", groupSummary(hangar), want)
		}
		if _, has := hangar["owned"]; has || hangar["instanceId"] != goodInstance || hangar["timestamp"] == "2026-11-16T07:04:11.406Z" {
			t.Errorf("saved hangar = %v", hangar)
		}
		if opfor["instanceId"] != res.OpForInstance || opfor["name"] != "N - OpFor" {
			t.Errorf("saved opfor = %v", opfor)
		}
		forces := fs.op["forces"].([]any)
		f0, f1 := forces[0].(map[string]any), forces[1].(map[string]any)
		if fs.op["operationId"] != res.OperationID || f0["instanceId"] != goodInstance || f0["alignment"] != "friendly" ||
			f0["timestamp"] != hangar["timestamp"] || f1["instanceId"] != res.OpForInstance || f1["alignment"] != "enemy" || f1["timestamp"] != opfor["timestamp"] {
			t.Errorf("operation = %v", fs.op)
		}
	})

	t.Run("keep the existing OpFor", func(t *testing.T) {
		fs := &fakeServer{}
		ts := httptest.NewServer(fs.handler(t))
		defer ts.Close()
		cl := &MekBayClient{URL: "ws" + strings.TrimPrefix(ts.URL, "http")}
		res, err := cl.SyncOperation(context.Background(), OperationRequest{
			OwnerUID: ownerUID, CampaignInstance: goodInstance, DeployedUnitIDs: []string{"u1"}, OpFor: d,
			OpForInstance: fakeOpForID, KeepOpFor: true, OperationName: "N", OperationID: "op-existing",
		})
		if err != nil {
			t.Fatal(err)
		}
		fs.mu.Lock()
		defer fs.mu.Unlock()
		if got := strings.Join(fs.actions, ","); got != "register,getForce,saveForce,getForce,saveOperation,getOperation" {
			t.Fatalf("actions = %s (the OpFor must not be saved over)", got)
		}
		f1 := fs.op["forces"].([]any)[1].(map[string]any)
		if res.OpForInstance != fakeOpForID || res.OperationID != "op-existing" || f1["instanceId"] != fakeOpForID || f1["timestamp"] != "2026-11-01T00:00:00.000Z" {
			t.Errorf("result = %+v, operation = %v", res, fs.op)
		}
	})

	t.Run("leave the hangar alone", func(t *testing.T) {
		fs := &fakeServer{}
		ts := httptest.NewServer(fs.handler(t))
		defer ts.Close()
		cl := &MekBayClient{URL: "ws" + strings.TrimPrefix(ts.URL, "http")}
		if _, err := cl.SyncOperation(context.Background(), OperationRequest{OwnerUID: ownerUID, CampaignInstance: goodInstance, OpFor: d, OperationName: "N"}); err != nil {
			t.Fatal(err)
		}
		fs.mu.Lock()
		defer fs.mu.Unlock()
		if got := strings.Join(fs.actions, ","); got != "register,getForce,saveForce,saveOperation,getOperation" || len(fs.saved) != 1 {
			t.Fatalf("with no deployment only the OpFor is saved: %s", got)
		}
	})

	t.Run("read the hangar anonymously", func(t *testing.T) {
		fs := &fakeServer{}
		ts := httptest.NewServer(fs.handler(t))
		defer ts.Close()
		cl := &MekBayClient{URL: "ws" + strings.TrimPrefix(ts.URL, "http")}
		h, err := cl.ReadHangar(context.Background(), goodInstance)
		if err != nil || len(h.Units) != 2 || h.Units[0].Unit != "BMGrizzly" || h.Units[0].Group != "Alpha" || h.Units[0].Gunnery != 4 {
			t.Fatalf("ReadHangar = %+v, %v", h, err)
		}
		if _, err := cl.ReadHangar(context.Background(), "no-such-force"); err == nil {
			t.Error("a missing force should be an error")
		}
		fs.mu.Lock()
		defer fs.mu.Unlock()
		if !uuidPattern.MatchString(fs.uuids[0]) || fs.uuids[0] == ownerUID {
			t.Errorf("a read should use a throwaway identity, registered as %q", fs.uuids[0])
		}
		if len(fs.saved) != 0 {
			t.Error("reading must never save")
		}
	})
}

func TestMekBayClientReportsRefusals(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := websocket.Accept(w, r, nil)
		defer c.CloseNow()
		for {
			_, b, err := c.Read(r.Context())
			if err != nil {
				return
			}
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			rid, _ := m["requestId"].(string)
			var out map[string]any
			switch m["action"] {
			case "getForce":
				out = map[string]any{"action": "forceData", "requestId": rid, "data": decodeForce(t, fakeHangar)}
			case "saveForce":
				out = map[string]any{"action": "warning", "requestId": rid, "code": "not_owner", "message": "You do not own this force"}
			default:
				continue
			}
			bb, _ := json.Marshal(out)
			_ = c.Write(r.Context(), websocket.MessageText, bb)
		}
	}))
	defer ts.Close()
	cl := &MekBayClient{URL: "ws" + strings.TrimPrefix(ts.URL, "http")}
	_, err := cl.SyncOperation(context.Background(), OperationRequest{OwnerUID: ownerUID, CampaignInstance: goodInstance, DeployedUnitIDs: []string{"u1"}, OperationName: "N"})
	if err == nil || !strings.Contains(err.Error(), "doesn't own") {
		t.Fatalf("expected a not-owner error, got %v", err)
	}
}

// TestMekBayLive talks to the real mekbay.com. It reads the hangar and creates an OpFor force and an operation in the
// given account, but leaves the hangar's groups alone. It only runs when MEKBAY_LIVE_UID and MEKBAY_LIVE_INSTANCE (the
// account's UID and one of its saved forces) are set.
func TestMekBayLive(t *testing.T) {
	uid, instance := os.Getenv("MEKBAY_LIVE_UID"), os.Getenv("MEKBAY_LIVE_INSTANCE")
	if uid == "" || instance == "" {
		t.Skip("set MEKBAY_LIVE_UID and MEKBAY_LIVE_INSTANCE to run against mekbay.com")
	}
	cl := &MekBayClient{}
	h, err := cl.ReadHangar(context.Background(), instance)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("hangar %q has %d units", h.Name, len(h.Units))
	for _, v := range hangarViews(h) {
		t.Logf("  %s: %s %d BV, pilot %q %d/%d, damaged=%v, deployed=%v", v.ID, v.Name, v.BV, v.Pilot, v.Gunnery, v.Piloting, v.Damaged, v.InDeployed)
	}
	lib := Library{Root: "../../campaigns"}
	c, _ := lib.Campaign("refusalwar")
	sc, _ := c.Scenario("scenario_000")
	draft, err := opForDraft(c, sc)
	if err != nil {
		t.Fatal(err)
	}
	res, err := cl.SyncOperation(context.Background(), OperationRequest{
		OwnerUID: uid, CampaignInstance: instance, OpFor: draft, OperationName: operationName(c, sc) + " (live test)",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("operation %s, opfor force %s", OperationURL(res.OperationID), res.OpForInstance)
}
