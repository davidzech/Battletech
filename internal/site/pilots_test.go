package site

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/pilot"
	"github.com/davidzech/Battletech/internal/storage"
)

// startForm is what the start wizard posts: the hangar link plus, for each name, a pilot who allocates all 150 of
// their starting SP (90 to Gunnery, 60 to Edge tokens).
func startForm(hangar string, names ...string) url.Values {
	v := url.Values{"campaign": {"refusalwar"}, "hangar": {hangar}, "players": {"1"}, "force_name": {"Wolf's Dragoons"}}
	for i, n := range names {
		p := "pilot" + strconv.Itoa(i) + "_"
		v.Set(p+"name", n)
		v.Set(p+"gunnery", "90")
		v.Set(p+"edge_tokens", "60")
	}
	return v
}

// startedCompany sets up a test server with a company that has started Refusal War.
func startedCompany(t *testing.T) (*httptest.Server, *storage.Store, *http.Cookie) {
	t.Helper()
	srv, store := newTestServer(t)
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	if r := post(t, srv, "/api/account", url.Values{"account": {"Wolf's Dragoons"}}, cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("company: %d", r.StatusCode)
	}
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	return srv, store, cookie
}

func TestNamedPilotsFlow(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()
	pilots := func(extra ...string) url.Values {
		v := url.Values{"campaign": {"refusalwar"}}
		for i := 0; i+1 < len(extra); i += 2 {
			v.Set(extra[i], extra[i+1])
		}
		return v
	}

	// The endpoints need a seed, a named company and a started campaign.
	if r := post(t, srv, "/api/pilots", pilots("name", "Alrek"), nil, srv.URL); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("no seed: %d", r.StatusCode)
	}
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	if r := post(t, srv, "/api/pilots", pilots("name", "Alrek"), cookie, srv.URL); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("no company: %d", r.StatusCode)
	}
	post(t, srv, "/api/account", url.Values{"account": {"Wolf's Dragoons"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/pilots", pilots("name", "Alrek"), cookie, srv.URL); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("campaign not started: %d", r.StatusCode)
	}
	if c := body(t, srv, "/refusalwar/", cookie); strings.Contains(c, `id="pilots"`) {
		t.Fatal("the pilots panel belongs to a started campaign")
	}
	id := playthroughID("wolfpack2026")
	// A campaign started without pilots (before they were part of starting one) has to name them afterwards.
	if err := store.StartRun(ctx, id, "refusalwar", mekbay.Hangar{URL: goodHangarURL, Instance: goodInstance}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`UPDATE campaign_runs SET warchest = 5000`); err != nil { // recruiting costs SP
		t.Fatal(err)
	}

	// A started campaign asks for its two starting pilots.
	if c := body(t, srv, "/refusalwar/manage/", cookie); !strings.Contains(c, `id="pilots"`) || strings.Contains(c, "needs 2 named pilots") || strings.Contains(c, `id="pilot-add"`) {
		t.Fatalf("pilots panel: %.2500s", c)
	}

	// Bad requests change nothing.
	for name, c := range map[string]struct {
		form   url.Values
		origin string
		status int
	}{
		"cross-origin":     {pilots("name", "Alrek"), "https://evil.example", http.StatusForbidden},
		"blank name":       {pilots("name", "  "), srv.URL, http.StatusBadRequest},
		"markup in a name": {pilots("name", "<b>x</b>"), srv.URL, http.StatusBadRequest},
		"unknown campaign": {url.Values{"campaign": {"nope"}, "name": {"Alrek"}}, srv.URL, http.StatusNotFound},
	} {
		if r := post(t, srv, "/api/pilots", c.form, cookie, c.origin); r.StatusCode != c.status {
			t.Errorf("%s: got %d, want %d", name, r.StatusCode, c.status)
		}
	}
	if ps, _ := store.Pilots(ctx, id, "refusalwar"); len(ps) != 0 {
		t.Fatalf("refused requests created pilots: %+v", ps)
	}

	// Naming pilots, up to the limit of four.
	for _, name := range []string{"Alrek", "Katarina", "Jarek", "Sybille"} {
		if r := post(t, srv, "/api/pilots", pilots("name", name), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
			t.Fatalf("add %s: %d", name, r.StatusCode)
		}
	}
	if r := post(t, srv, "/api/pilots", pilots("name", "Fifth"), cookie, srv.URL); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("a fifth pilot should be refused: %d", r.StatusCode)
	}
	c := body(t, srv, "/refusalwar/manage/", cookie)
	if strings.Contains(c, "needs 2 named pilots") || strings.Contains(c, `id="pilot-add"`) || !strings.Contains(c, "Alrek") || !strings.Contains(c, "Sybille") {
		t.Fatalf("with four pilots there is nothing left to ask for: %.3000s", c)
	}
	ps, _ := store.Pilots(ctx, id, "refusalwar")
	alrek := ps[0]
	// A pilot added through the endpoint starts with the campaign's 150 SP, all of it unallocated unless the request spends it.
	if alrek.Name != "Alrek" || alrek.Earned() != 150 || alrek.Unallocated() != 150 || !strings.Contains(c, `data-pilot="`+alrek.ID+`"`) {
		t.Fatalf("alrek = %+v", alrek)
	}

	// SP earned and allocated (the allocate endpoint, with its validation, is covered by TestSPEndpoints).
	if err := store.AwardSP(ctx, id, "refusalwar", alrek.ID, pilot.SPAllocation{Gunnery: 300, EdgeTokens: 60}); err != nil {
		t.Fatalf("award: %v", err)
	}
	ps, _ = store.Pilots(ctx, id, "refusalwar")
	if ps[0].SP != (pilot.SPAllocation{Gunnery: 300, EdgeTokens: 60}) {
		t.Fatalf("allocation = %+v", ps[0].SP)
	}
	c = body(t, srv, "/refusalwar/manage/", cookie)
	if !strings.Contains(c, `<span class="stat-value">3</span>`) || !strings.Contains(c, "SP earned <b>510</b>") || !strings.Contains(c, `stat-next">300/700 SP<`) || !strings.Contains(c, "<b>2</b>") || !strings.Contains(c, `stat-bar"><span style="width: 43%"`) {
		t.Fatalf("the page should show the new levels (gunnery 3, edge 2): %.4000s", c)
	}

	// A fallen pilot is marked as such, can't earn more, and frees their place.
	if r := post(t, srv, "/api/pilots/fallen", pilots("pilot", ps[1].ID), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("fallen: %d", r.StatusCode)
	}
	if r := post(t, srv, "/api/pilots/fallen", pilots("pilot", ps[1].ID), cookie, srv.URL); r.StatusCode != http.StatusNotFound {
		t.Fatalf("already fallen: %d", r.StatusCode)
	}
	if got := post(t, srv, "/api/pilots/allocate", pilots("pilot", ps[1].ID, "earned", "100"), cookie, srv.URL).StatusCode; got != http.StatusNotFound {
		t.Errorf("SP earned by the fallen: %d", got)
	}
	if c := body(t, srv, "/refusalwar/manage/", cookie); !strings.Contains(c, "Killed in Action") || !strings.Contains(c, `class="rec-revive"`) || strings.Contains(c, `id="pilot-add"`) {
		t.Fatalf("a fallen pilot should show, and free a place: %.3000s", c)
	}
	if r := post(t, srv, "/api/pilots", pilots("name", "Replacement"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("replacement: %d", r.StatusCode)
	}
}

func TestStartWizardSubmission(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf's Dragoons"}}, cookie, srv.URL)
	id := playthroughID("wolfpack2026")

	// The wizard page carries what the script needs: the pilot count, the SP budget and the ladders.
	page := body(t, srv, "/refusalwar/manage/", cookie)
	for _, want := range []string{`id="start-wizard"`, `data-pilots="2"`, `data-sp="150"`, `data-ladders="[{&#34;key&#34;:&#34;gunnery&#34;`, `id="wiz-progress"`, `id="wizard-open"`} {
		if !strings.Contains(page, want) {
			t.Errorf("wizard page is missing %s", want)
		}
	}
	if strings.Contains(page, `id="run-dialog"`) {
		t.Error("an unstarted campaign has no change-hangar dialog")
	}
	// The starting rules are a short bulleted list, not one dense sentence.
	for _, want := range []string{`class="start-rules"`, `<li>Your hangar holds <b>at least 5 mechs</b>, totalling <b>up to 10,000 BV</b> (base BV, whatever the pilots&rsquo; skills).</li>`, `<li>You create <b>2 named pilots</b>, each allocating <b>150 SP</b>.</li>`, `<li>You name your force, then pick the mechs it fields from the unit list`} {
		if !strings.Contains(page, want) {
			t.Errorf("starting rules are missing %s", want)
		}
	}
	if strings.Contains(page, "takes a few steps") {
		t.Error("the dense starting sentence should be gone")
	}
	// The wizard lists the hangar's mechs with the shared row component, and the icons are credited.
	if i, j := strings.Index(page, "/static/mechrow.js"), strings.Index(page, "/static/campaign.js"); i < 0 || j < 0 || i > j {
		t.Error("the campaign page should load mechrow.js before campaign.js")
	}
	if !strings.Contains(page, "MegaMek Data") || !strings.Contains(page, "CC BY-NC-SA 4.0") {
		t.Error("the footer should credit the unit icons")
	}

	with := func(mut func(v url.Values)) url.Values {
		v := startForm(goodHangar, "Alrek", "Katarina")
		mut(v)
		return v
	}
	for name, form := range map[string]url.Values{
		"no pilots at all":  {"campaign": {"refusalwar"}, "hangar": {goodHangar}},
		"only one pilot":    with(func(v url.Values) { v.Del("pilot1_name") }),
		"blank name":        with(func(v url.Values) { v.Set("pilot1_name", "  ") }),
		"same name twice":   with(func(v url.Values) { v.Set("pilot1_name", "alrek") }),
		"one SP short":      with(func(v url.Values) { v.Set("pilot0_gunnery", "89") }),
		"one SP too many":   with(func(v url.Values) { v.Set("pilot0_gunnery", "91") }),
		"nothing allocated": with(func(v url.Values) { v.Del("pilot1_gunnery"); v.Del("pilot1_edge_tokens") }),
		"negative SP":       with(func(v url.Values) { v.Set("pilot0_gunnery", "-10"); v.Set("pilot0_piloting", "160") }),
		"nonsense SP":       with(func(v url.Values) { v.Set("pilot0_piloting", "lots") }),
		"bad hangar":        with(func(v url.Values) { v.Set("hangar", "https://evil.example/?instance="+goodInstance) }),
		"markup in a name":  with(func(v url.Values) { v.Set("pilot0_name", "<b>x</b>") }),
	} {
		if r := post(t, srv, "/api/campaign/start", form, cookie, srv.URL); r.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", name, r.StatusCode)
		}
	}
	if _, ok, _ := store.Run(ctx, id, "refusalwar"); ok {
		t.Fatal("a rejected start must not start the campaign")
	}
	if ps, _ := store.Pilots(ctx, id, "refusalwar"); len(ps) != 0 {
		t.Fatalf("a rejected start must not create pilots: %+v", ps)
	}

	// Splitting the SP any way is fine as long as all of it is allocated.
	good := with(func(v url.Values) {
		v.Set("pilot1_gunnery", "0")
		v.Set("pilot1_edge_tokens", "0")
		v.Set("pilot1_piloting", "100")
		v.Set("pilot1_edge_abilities", "50")
	})
	if r := post(t, srv, "/api/campaign/start", good, cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("good start: %d", r.StatusCode)
	}
	ps, _ := store.Pilots(ctx, id, "refusalwar")
	if len(ps) != 2 || ps[0].Name != "Alrek" || ps[0].SP != (pilot.SPAllocation{Gunnery: 90, EdgeTokens: 60}) ||
		ps[1].Name != "Katarina" || ps[1].SP != (pilot.SPAllocation{Piloting: 100, EdgeAbilities: 50}) {
		t.Fatalf("pilots = %+v", ps)
	}
	if ps[0].EdgeTokens() != 2 || ps[1].Piloting() != 4 || ps[1].EdgeAbilities() != 0 {
		t.Errorf("levels follow the ladders: %+v", ps)
	}
	if c := body(t, srv, "/refusalwar/manage/", cookie); !strings.Contains(c, "Katarina") || strings.Contains(c, "needs 2 named pilots") || strings.Contains(c, `id="start-wizard"`) || strings.Contains(c, `id="run-dialog"`) {
		t.Fatalf("a started campaign shows its pilots and no change-link dialog: %.3000s", c)
	}

	// Submitting the wizard again is refused, and duplicates nothing.
	other := "https://mekbay.com/?instance=01a12f98-8a21-7d12-b43d-52bb70872376"
	if r := post(t, srv, "/api/campaign/start", startForm(other, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusConflict {
		t.Fatalf("second submit: %d", r.StatusCode)
	}
	if ps, _ := store.Pilots(ctx, id, "refusalwar"); len(ps) != 2 {
		t.Fatalf("a second submit must not duplicate pilots: %+v", ps)
	}
}

func TestStartedCompanyHelper(t *testing.T) {
	_, store, _ := startedCompany(t)
	if run, ok, _ := store.Run(context.Background(), playthroughID("wolfpack2026"), "refusalwar"); !ok || !run.Started() {
		t.Fatal("helper should start the campaign")
	}
}

func TestDeleteCampaign(t *testing.T) {
	srv, store, cookie := startedCompany(t)
	ctx := context.Background()
	id := playthroughID("wolfpack2026")
	form := url.Values{"campaign": {"refusalwar"}}

	if r := post(t, srv, "/api/campaign/delete", form, cookie, "https://evil.example"); r.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin delete: %d", r.StatusCode)
	}
	if _, ok, _ := store.Run(ctx, id, "refusalwar"); !ok {
		t.Fatal("a refused delete removed the campaign")
	}
	// The page offers the delete button with its confirmation popup while the campaign is started.
	if page := body(t, srv, "/refusalwar/manage/", cookie); !strings.Contains(page, `id="delete-dialog"`) {
		t.Error("a started campaign should show the delete button and its confirmation popup")
	}

	if r := post(t, srv, "/api/campaign/delete", form, cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", r.StatusCode)
	}
	if _, ok, _ := store.Run(ctx, id, "refusalwar"); ok {
		t.Error("the run should be gone")
	}
	if ps, _ := store.Pilots(ctx, id, "refusalwar"); len(ps) != 0 {
		t.Errorf("the pilots should be gone: %v", ps)
	}
	if page := body(t, srv, "/refusalwar/", cookie); strings.Contains(page, `id="delete-dialog"`) || !strings.Contains(page, `id="wizard-open"`) {
		t.Error("after deleting, the campaign should offer to start again and no delete button")
	}
	// Deleting what isn't started is refused, and the company survives.
	if r := post(t, srv, "/api/campaign/delete", form, cookie, srv.URL); r.StatusCode != http.StatusBadRequest {
		t.Errorf("deleting a campaign that isn't started: %d", r.StatusCode)
	}
	if pt, ok, _ := store.Playthrough(ctx, id); !ok || !pt.Configured() {
		t.Error("the company should survive deleting a campaign")
	}
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Errorf("starting again after deleting: %d", r.StatusCode)
	}
}

func TestManageAndOverview(t *testing.T) {
	srv, _, cookie := startedCompany(t)
	// The overview is compressed: the pilots as a table, a place for the units, a link to the manage page, and none of the
	// management controls.
	over := body(t, srv, "/refusalwar/", cookie)
	for _, want := range []string{`id="force-overview"`, `id="force-units"`, `href="/refusalwar/manage/"`, "Manage force", "Alrek", "Katarina", `/portraits/`} {
		if !strings.Contains(over, want) {
			t.Errorf("overview is missing %q", want)
		}
	}
	for _, not := range []string{`id="pilot-add"`, `id="delete-dialog"`, `id="run-dialog"`, `class="pilot-sp"`} {
		if strings.Contains(over, not) {
			t.Errorf("overview should not hold %q", not)
		}
	}
	// The manage page holds all of them.
	man := body(t, srv, "/refusalwar/manage/", cookie)
	for _, want := range []string{`id="delete-dialog"`, `id="sp-dialog"`, `class="button pilot-sp"`, "Purchase New Units"} {
		if !strings.Contains(man, want) {
			t.Errorf("manage page is missing %q", want)
		}
	}

	// The unit summary: every unit with the base BV, and their total.
	r := get(t, srv, "/api/campaign/force?campaign=refusalwar", cookie)
	var out struct {
		Units []hangarUnitView `json:"units"`
		BV    int              `json:"bv"`
	}
	if r.StatusCode != http.StatusOK {
		t.Fatalf("force: %d", r.StatusCode)
	}
	if err := json.NewDecoder(r.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	sum := 0
	for _, u := range out.Units {
		if !u.Unknown {
			sum += u.BV
		}
	}
	if len(out.Units) == 0 || out.BV != sum || out.BV == 0 {
		t.Errorf("force summary: %d units, bv %d (sum %d)", len(out.Units), out.BV, sum)
	}
	if r := get(t, srv, "/api/campaign/force?campaign=nope", cookie); r.StatusCode != http.StatusNotFound {
		t.Errorf("unknown campaign: %d", r.StatusCode)
	}
	if r := get(t, srv, "/api/campaign/force?campaign=refusalwar", nil); r.StatusCode != http.StatusBadRequest {
		t.Errorf("no seed: %d", r.StatusCode)
	}
}

func TestManageNeedsAStartedCampaign(t *testing.T) {
	srv, _ := newTestServer(t)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	// No seed at all: back to the campaign page.
	r, err := client.Get(srv.URL + "/refusalwar/manage/")
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/refusalwar/" {
		t.Errorf("no seed: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	// A company that hasn't started the campaign: the same.
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	req, _ := http.NewRequest("GET", srv.URL+"/refusalwar/manage/", nil)
	req.AddCookie(cookie)
	if r, err := client.Do(req); err != nil || r.StatusCode != http.StatusSeeOther {
		t.Errorf("not started: %v %v", r, err)
	}
	if r, _ := client.Get(srv.URL + "/nope/manage/"); r.StatusCode != http.StatusNotFound {
		t.Errorf("unknown campaign: %d", r.StatusCode)
	}
}

func TestSPEndpoints(t *testing.T) {
	srv, store, cookie := startedCompany(t)
	ctx := context.Background()
	id := playthroughID("wolfpack2026")
	ps, _ := store.Pilots(ctx, id, "refusalwar")
	pl := ps[0].ID
	form := func(kv ...string) url.Values {
		v := url.Values{"campaign": {"refusalwar"}, "pilot": {pl}}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Set(kv[i], kv[i+1])
		}
		return v
	}
	status := func(path string, v url.Values) int { return post(t, srv, path, v, cookie, srv.URL).StatusCode }
	for name, c := range map[string]struct {
		path   string
		v      url.Values
		status int
	}{
		"earned SP, nothing":        {"/api/pilots/allocate", form(), http.StatusBadRequest},
		"earned SP, negative":       {"/api/pilots/allocate", form("earned", "-5"), http.StatusBadRequest},
		"earned SP, text":           {"/api/pilots/allocate", form("earned", "lots"), http.StatusBadRequest},
		"allocate with none":        {"/api/pilots/allocate", form("gunnery", "10"), http.StatusBadRequest},
		"allocate zero":             {"/api/pilots/allocate", form("gunnery", "0"), http.StatusBadRequest},
		"earned SP, for a stranger": {"/api/pilots/allocate", form("pilot", "nobody", "earned", "10"), http.StatusNotFound},
		"cross-origin earned SP":    {"/api/pilots/allocate", form("earned", "10"), http.StatusForbidden},
	} {
		var got int
		if name == "cross-origin earned SP" {
			got = post(t, srv, c.path, c.v, cookie, "https://evil.example").StatusCode
		} else {
			got = status(c.path, c.v)
		}
		if got != c.status {
			t.Errorf("%s: got %d, want %d", name, got, c.status)
		}
	}
	if got := status("/api/pilots/allocate", form("earned", "300")); got != http.StatusNoContent {
		t.Fatalf("earn: %d", got)
	}
	if got := status("/api/pilots/allocate", form("gunnery", "200", "piloting", "101")); got != http.StatusBadRequest {
		t.Errorf("over-allocating: %d", got)
	}
	if got := status("/api/pilots/allocate", form("gunnery", "200", "piloting", "100")); got != http.StatusNoContent {
		t.Fatalf("allocate: %d", got)
	}
	ps, _ = store.Pilots(ctx, id, "refusalwar")
	if p := ps[0]; p.TotalSP != 450 || p.Unallocated() != 0 || p.SP.Gunnery != 290 || p.SP.Piloting != 100 {
		t.Errorf("after earn and allocate: %+v", p)
	}
	// The dialog stages earned SP and commits it with the allocation in one request.
	if got := status("/api/pilots/allocate", form("earned", "50", "gunnery", "51")); got != http.StatusBadRequest {
		t.Errorf("allocating more than was earned: %d", got)
	}
	if got := status("/api/pilots/allocate", form("earned", "lots")); got != http.StatusBadRequest {
		t.Errorf("a bad earned amount: %d", got)
	}
	if got := status("/api/pilots/allocate", form("earned", "50", "gunnery", "50")); got != http.StatusNoContent {
		t.Fatalf("earn and allocate together: %d", got)
	}
	if got := status("/api/pilots/allocate", form("earned", "20")); got != http.StatusNoContent {
		t.Fatalf("earn without allocating: %d", got)
	}
	ps, _ = store.Pilots(ctx, id, "refusalwar")
	if p := ps[0]; p.TotalSP != 520 || p.Unallocated() != 20 || p.SP.Gunnery != 340 {
		t.Errorf("after staged earning was applied: %+v", p)
	}
}

// A database from before pilots had a total gets one: every SP they had was allocated, so the total is the columns' sum.
func TestMigrateSPTotal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	s, err := storage.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := playthroughID("oldseed99")
	if _, err := s.EnsurePlaythrough(ctx, id); err != nil {
		t.Fatal(err)
	}
	// An old row: the columns hold SP, the total was never written.
	if _, err := s.DB().Exec(`INSERT INTO named_pilots (id, playthrough_id, campaign, name, sp_gunnery, sp_edge_tokens) VALUES ('p1', ?, 'refusalwar', 'Alrek', 90, 60)`, id); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = storage.OpenSQLite(path) // opening again runs the schema fix-ups
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ps, err := s.Pilots(ctx, id, "refusalwar")
	if err != nil || len(ps) != 1 || ps[0].TotalSP != 150 || ps[0].Unallocated() != 0 {
		t.Fatalf("migrated pilot: %+v %v", ps, err)
	}
	// And it leaves a consistent pilot alone: unallocated SP survives a reopen.
	if err := s.EarnSP(ctx, id, "refusalwar", "p1", 40); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if s, err = storage.OpenSQLite(path); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if ps, _ := s.Pilots(ctx, id, "refusalwar"); ps[0].TotalSP != 190 || ps[0].Unallocated() != 40 {
		t.Fatalf("after reopening: %+v", ps[0])
	}
}

func TestReviveAndDeletePilots(t *testing.T) {
	srv, store, cookie := startedCompany(t)
	ctx := context.Background()
	id := playthroughID("wolfpack2026")
	ps, _ := store.Pilots(ctx, id, "refusalwar")
	a, b := ps[0], ps[1]
	form := func(pl string) url.Values { return url.Values{"campaign": {"refusalwar"}, "pilot": {pl}} }
	do := func(path, pilot string) int { return post(t, srv, path, form(pilot), cookie, srv.URL).StatusCode }

	if got := post(t, srv, "/api/pilots/revive", form(a.ID), cookie, "https://evil.example").StatusCode; got != http.StatusForbidden {
		t.Errorf("cross-origin revive: %d", got)
	}
	if got := do("/api/pilots/revive", a.ID); got != http.StatusNotFound {
		t.Errorf("reviving a living pilot: %d", got)
	}
	// Killed in action: moved from the active list to the memorial, still in the records, and revivable.
	if got := do("/api/pilots/fallen", a.ID); got != http.StatusNoContent {
		t.Fatalf("kia: %d", got)
	}
	page := body(t, srv, "/refusalwar/manage/", cookie)
	if !strings.Contains(page, "Memorial") || strings.Contains(page, `<li class="pilot" data-pilot="`+a.ID) || !strings.Contains(page, `<li class="pilot kia" data-pilot="`+a.ID) || !strings.Contains(page, `<li class="pilot" data-pilot="`+b.ID) {
		t.Error("the killed pilot should leave the active list for the memorial, and stay in the records")
	}
	if over := body(t, srv, "/refusalwar/", cookie); strings.Contains(over, a.Name+"</b>") {
		t.Error("the overview should not show a pilot killed in action")
	}
	if got := do("/api/pilots/revive", a.ID); got != http.StatusNoContent {
		t.Fatalf("revive: %d", got)
	}
	if ps, _ := store.Pilots(ctx, id, "refusalwar"); ps[0].Fallen || ps[0].ID != a.ID {
		t.Errorf("revived pilot: %+v", ps[0])
	}
	// A portrait taken by someone else in the meantime is swapped for a free one.
	if got := do("/api/pilots/fallen", a.ID); got != http.StatusNoContent {
		t.Fatal(got)
	}
	if _, err := store.DB().Exec(`UPDATE named_pilots SET portrait = ? WHERE id = ?`, a.Portrait, b.ID); err != nil {
		t.Fatal(err)
	}
	if got := do("/api/pilots/revive", a.ID); got != http.StatusNoContent {
		t.Fatalf("revive with a clash: %d", got)
	}
	ps, _ = store.Pilots(ctx, id, "refusalwar")
	if ps[0].Portrait == ps[1].Portrait || !pilot.ValidPortrait(ps[0].Portrait) {
		t.Errorf("portraits after a clash: %q %q", ps[0].Portrait, ps[1].Portrait)
	}
	// The limit of living pilots still holds.
	for _, n := range []string{"C", "D"} {
		if _, err := store.AddPilot(ctx, id, "refusalwar", n, "", pilot.SPAllocation{}, pilot.MaxNamedPilots); err != nil {
			t.Fatal(err)
		}
	}
	if got := do("/api/pilots/fallen", a.ID); got != http.StatusNoContent {
		t.Fatal(got)
	}
	if _, err := store.AddPilot(ctx, id, "refusalwar", "E", "", pilot.SPAllocation{}, pilot.MaxNamedPilots); err != nil {
		t.Fatal(err)
	}
	if got := do("/api/pilots/revive", a.ID); got != http.StatusBadRequest {
		t.Errorf("reviving with a full command: %d", got)
	}
	// Deleting removes a pilot for good, living or killed, and only in their own campaign.
	if got := do("/api/pilots/delete", a.ID); got != http.StatusNoContent {
		t.Fatalf("delete a killed pilot: %d", got)
	}
	if got := do("/api/pilots/delete", b.ID); got != http.StatusNoContent {
		t.Fatalf("delete a living pilot: %d", got)
	}
	if got := do("/api/pilots/delete", b.ID); got != http.StatusNotFound {
		t.Errorf("deleting twice: %d", got)
	}
	ps, _ = store.Pilots(ctx, id, "refusalwar")
	for _, p := range ps {
		if p.ID == a.ID || p.ID == b.ID {
			t.Errorf("deleted pilot is still there: %+v", p)
		}
	}
}

func TestRepairEndpoints(t *testing.T) {
	srv, store, cookie := startedCompany(t)
	ctx := context.Background()
	id := playthroughID("wolfpack2026")
	ps, _ := store.Pilots(ctx, id, "refusalwar")
	pl := ps[0].ID
	form := func(kv ...string) url.Values {
		v := url.Values{"campaign": {"refusalwar"}, "pilot": {pl}}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Set(kv[i], kv[i+1])
		}
		return v
	}
	status := func(path string, v url.Values) int { return post(t, srv, path, v, cookie, srv.URL).StatusCode }

	if got := post(t, srv, "/api/pilots/reset", form(), cookie, "https://evil.example").StatusCode; got != http.StatusForbidden {
		t.Errorf("cross-origin reset: %d", got)
	}
	// The records dialog offers both repairs for every pilot.
	page := body(t, srv, "/refusalwar/manage/", cookie)
	for _, want := range []string{`class="rec-reset"`, `class="rec-total"`, `class="rec-total-form"`, `data-allocated="150"`, `data-total="150"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the records dialog is missing %q", want)
		}
	}
	for name, c := range map[string]struct {
		v      url.Values
		status int
	}{
		"no total":        {form(), http.StatusBadRequest},
		"negative total":  {form("total", "-1"), http.StatusBadRequest},
		"text total":      {form("total", "lots"), http.StatusBadRequest},
		"absurd total":    {form("total", "99999999"), http.StatusBadRequest},
		"below allocated": {form("total", "100"), http.StatusConflict},
		"unknown pilot":   {form("pilot", "nobody", "total", "300"), http.StatusNotFound},
	} {
		if got := status("/api/pilots/total", c.v); got != c.status {
			t.Errorf("total, %s: got %d, want %d", name, got, c.status)
		}
	}
	if ps, _ := store.Pilots(ctx, id, "refusalwar"); ps[0].TotalSP != 150 || ps[0].SP.Total() != 150 {
		t.Fatalf("refused overrides changed the pilot: %+v", ps[0])
	}
	if got := status("/api/pilots/total", form("total", "400")); got != http.StatusNoContent {
		t.Fatalf("raise the total: %d", got)
	}
	if got := status("/api/pilots/total", form("total", "100", "reset", "1")); got != http.StatusNoContent {
		t.Fatalf("lower the total with the reset agreed: %d", got)
	}
	if ps, _ := store.Pilots(ctx, id, "refusalwar"); ps[0].TotalSP != 100 || ps[0].SP.Total() != 0 {
		t.Errorf("after the confirmed override: %+v", ps[0])
	}
	if got := status("/api/pilots/allocate", form("gunnery", "100")); got != http.StatusNoContent {
		t.Fatal(got)
	}
	if got := status("/api/pilots/reset", form()); got != http.StatusNoContent {
		t.Fatalf("reset: %d", got)
	}
	if ps, _ := store.Pilots(ctx, id, "refusalwar"); ps[0].TotalSP != 100 || ps[0].SP.Total() != 0 {
		t.Errorf("after the factory reset: %+v", ps[0])
	}
	if got := status("/api/pilots/reset", form("pilot", "nobody")); got != http.StatusNotFound {
		t.Errorf("reset of an unknown pilot: %d", got)
	}
}

func TestRenamePilots(t *testing.T) {
	srv, store, cookie := startedCompany(t)
	ctx := context.Background()
	id := playthroughID("wolfpack2026")
	ps, _ := store.Pilots(ctx, id, "refusalwar")
	a, b := ps[0], ps[1]
	rename := func(pl, name string) int {
		return post(t, srv, "/api/pilots/rename", url.Values{"campaign": {"refusalwar"}, "pilot": {pl}, "name": {name}}, cookie, srv.URL).StatusCode
	}
	if got := post(t, srv, "/api/pilots/rename", url.Values{"campaign": {"refusalwar"}, "pilot": {a.ID}, "name": {"X"}}, cookie, "https://evil.example").StatusCode; got != http.StatusForbidden {
		t.Errorf("cross-origin rename: %d", got)
	}
	for name, c := range map[string]struct {
		pilot, name string
		status      int
	}{
		"blank":       {a.ID, "   ", http.StatusBadRequest},
		"markup":      {a.ID, "<b>x</b>", http.StatusBadRequest},
		"too long":    {a.ID, strings.Repeat("a", 31), http.StatusBadRequest},
		"taken":       {a.ID, b.Name, http.StatusBadRequest},
		"taken, case": {a.ID, strings.ToUpper(b.Name), http.StatusBadRequest},
		"unknown":     {"nobody", "Valid", http.StatusNotFound},
	} {
		if got := rename(c.pilot, c.name); got != c.status {
			t.Errorf("rename, %s: got %d, want %d", name, got, c.status)
		}
	}
	if got := rename(a.ID, "Alrek Prime"); got != http.StatusNoContent {
		t.Fatalf("rename: %d", got)
	}
	if got := rename(a.ID, "ALREK PRIME"); got != http.StatusNoContent {
		t.Errorf("a pilot may change the case of their own name: %d", got)
	}
	// Killed pilots can be renamed too.
	if got := post(t, srv, "/api/pilots/fallen", url.Values{"campaign": {"refusalwar"}, "pilot": {b.ID}}, cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatal(got)
	}
	if got := rename(b.ID, "Katarina the Fallen"); got != http.StatusNoContent {
		t.Errorf("renaming a killed pilot: %d", got)
	}
	ps, _ = store.Pilots(ctx, id, "refusalwar")
	if ps[0].Name != "ALREK PRIME" || ps[1].Name != "Katarina the Fallen" {
		t.Errorf("names after renaming: %q %q", ps[0].Name, ps[1].Name)
	}
	if page := body(t, srv, "/refusalwar/manage/", cookie); strings.Count(page, `class="name-edit"`) != 3 || !strings.Contains(page, `class="name-edit"`) || !strings.Contains(page, `name-text">ALREK PRIME<`) {
		t.Error("the records dialog should offer the pencil and show the new name")
	}
}

// The stat boxes say how close each column is to its next level, as "SP/needed SP" under the level, with a progress bar and a tiny next level beside the number, and "max" at the top.
func TestStatBoxCaptions(t *testing.T) {
	srv, store, cookie := startedCompany(t)
	ctx := context.Background()
	id := playthroughID("wolfpack2026")
	ps, _ := store.Pilots(ctx, id, "refusalwar")
	// The starting pilots hold 90 or 60 SP in some columns: closeness shows as SP/needed.
	page := body(t, srv, "/refusalwar/manage/", cookie)
	if !strings.Contains(page, `stat-next">0/100 SP<`) || strings.Contains(page, "top level") || strings.Contains(page, "next:") || strings.Contains(page, " at 100 SP") {
		t.Errorf("captions should read next: SP/needed SP: %.1500s", page)
	}
	if err := store.AwardSP(ctx, id, "refusalwar", ps[0].ID, pilot.SPAllocation{Gunnery: 1000000}); err != nil {
		t.Fatal(err)
	}
	if page := body(t, srv, "/refusalwar/manage/", cookie); !strings.Contains(page, `stat-next">max<`) {
		t.Error("a column at its top level should say max")
	}
}

func TestPilotRanksEndToEnd(t *testing.T) {
	srv, store, cookie := startedCompany(t)
	ctx := context.Background()
	id := playthroughID("wolfpack2026")
	ps, _ := store.Pilots(ctx, id, "refusalwar")
	pl := ps[0]
	form := func(kv ...string) url.Values {
		v := url.Values{"campaign": {"refusalwar"}, "pilot": {pl.ID}}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Set(kv[i], kv[i+1])
		}
		return v
	}
	status := func(v url.Values) int { return post(t, srv, "/api/pilots/rank", v, cookie, srv.URL).StatusCode }

	// Everyone starts as a MechWarrior, shown under the portrait, with every rank offered in the picker.
	page := body(t, srv, "/refusalwar/manage/", cookie)
	for _, want := range []string{`class="rank-btn"`, ">MechWarrior</button>", `id="rank-dialog"`, `<option value="Star Captain">`, `<option value="saKhan">`, `<option value="Khan">`, `data-rank="MechWarrior"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the manage page is missing %q", want)
		}
	}
	// The seven Clan ranks are offered twice: in the rank picker and in the Recruit New Pilot dialog.
	if strings.Count(page, "<option value=") != 14 {
		t.Errorf("each rank picker should list the seven Clan ranks (14 options in all), got %d", strings.Count(page, "<option value="))
	}

	if got := post(t, srv, "/api/pilots/rank", form("rank", "Khan"), cookie, "https://evil.example").StatusCode; got != http.StatusForbidden {
		t.Errorf("cross-origin: %d", got)
	}
	for name, c := range map[string]struct {
		v      url.Values
		status int
	}{
		"not a rank":    {form("rank", "General"), http.StatusBadRequest},
		"wrong case":    {form("rank", "khan"), http.StatusBadRequest},
		"blank":         {form("rank", ""), http.StatusBadRequest},
		"unknown pilot": {form("pilot", "nobody", "rank", "Khan"), http.StatusNotFound},
	} {
		if got := status(c.v); got != c.status {
			t.Errorf("%s: got %d, want %d", name, got, c.status)
		}
	}
	if ps, _ := store.Pilots(ctx, id, "refusalwar"); ps[0].Rank != "" {
		t.Fatalf("refused requests changed the rank: %q", ps[0].Rank)
	}
	if got := status(form("rank", "Star Captain")); got != http.StatusNoContent {
		t.Fatalf("set rank: %d", got)
	}
	page = body(t, srv, "/refusalwar/manage/", cookie)
	if !strings.Contains(page, ">Star Captain</button>") || !strings.Contains(page, `data-rank="Star Captain"`) {
		t.Error("the card should show the new rank")
	}
	if over := body(t, srv, "/refusalwar/", cookie); !strings.Contains(over, "Star Captain &middot; ") {
		t.Error("the overview should show the rank beside the SP")
	}
	// A pilot Killed in Action keeps their rank, shown (but not editable) in the memorial.
	if got := post(t, srv, "/api/pilots/fallen", form(), cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatal(got)
	}
	page = body(t, srv, "/refusalwar/manage/", cookie)
	if !strings.Contains(page, `<span class="rank-label">Star Captain</span>`) {
		t.Error("the memorial should show the rank as a label")
	}
}

// The stat boxes say "Abilities" (a full "Edge abilities" wraps); the slider rows keep the full name.
func TestStatBoxLabels(t *testing.T) {
	srv, _, cookie := startedCompany(t)
	page := body(t, srv, "/refusalwar/manage/", cookie)
	if !strings.Contains(page, `stat-label">Abilities<`) || strings.Contains(page, `stat-label">Edge abilities<`) {
		t.Error("the pilot cards' boxes should say Abilities")
	}
	if !strings.Contains(page, `&#34;name&#34;:&#34;Edge abilities&#34;`) || !strings.Contains(page, `&#34;short&#34;:&#34;Abilities&#34;`) {
		t.Errorf("the ladders JSON should keep the full name and add the short one")
	}
}

func TestRecruitNewPilot(t *testing.T) {
	srv, store, cookie := startedCompany(t)
	ctx := context.Background()
	id := playthroughID("wolfpack2026")
	recruit := func(kv ...string) int {
		v := url.Values{"campaign": {"refusalwar"}}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Set(kv[i], kv[i+1])
		}
		return post(t, srv, "/api/pilots", v, cookie, srv.URL).StatusCode
	}
	// The button sits under the active pilots (above the memorial) and opens the one-step dialog.
	page := body(t, srv, "/refusalwar/manage/", cookie)
	for _, want := range []string{`id="recruit-open"`, ">Recruit New Pilot</button>", `id="recruit-dialog"`, `id="recruit-name"`, `id="recruit-rank"`, `id="recruit-portrait"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the manage page is missing %q", want)
		}
	}
	if strings.Contains(page, `id="recruit-open" disabled`) {
		t.Error("with room for another pilot the button is enabled")
	}
	if got := post(t, srv, "/api/pilots", url.Values{"campaign": {"refusalwar"}, "name": {"Jarek"}}, cookie, "https://evil.example").StatusCode; got != http.StatusForbidden {
		t.Errorf("cross-origin: %d", got)
	}

	ps, _ := store.Pilots(ctx, id, "refusalwar")
	existing := ps[0]
	for name, c := range map[string][]string{
		"blank name":     {"name", "  "},
		"markup":         {"name", "<b>x</b>"},
		"taken name":     {"name", existing.Name},
		"taken, case":    {"name", strings.ToUpper(existing.Name)},
		"bad rank":       {"name", "Jarek", "rank", "General"},
		"bad portrait":   {"name", "Jarek", "portrait", "../x.png"},
		"taken portrait": {"name", "Jarek", "portrait", existing.Portrait},
	} {
		if got := recruit(c...); got != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", name, got)
		}
	}
	if ps, _ := store.Pilots(ctx, id, "refusalwar"); len(ps) != 2 {
		t.Fatalf("refused requests added pilots: %d", len(ps))
	}

	free := ""
	for _, p := range pilot.Portraits() {
		if p.File != ps[0].Portrait && p.File != ps[1].Portrait {
			free = p.File
			break
		}
	}
	if got := recruit("name", "Jarek", "portrait", free, "rank", "Star Captain"); got != http.StatusNoContent {
		t.Fatalf("recruit: %d", got)
	}
	ps, _ = store.Pilots(ctx, id, "refusalwar")
	if len(ps) != 3 || ps[2].Name != "Jarek" || ps[2].Portrait != free || ps[2].Rank != "Star Captain" || ps[2].TotalSP != 150 || ps[2].SP.Total() != 0 || ps[2].Unallocated() != 150 {
		t.Fatalf("the new pilot: %+v", ps)
	}
	if v := ps[2].View(pilot.FactionRanks("clan")); v.Gunnery != 4 || v.Piloting != 5 || v.EdgeTokens != 1 || v.Abilities != 0 {
		t.Errorf("a recruit with nothing allocated is a standard 4/5 pilot with 1 Edge token: %+v", v)
	}
	// The 150 starting SP can be spent on the way in, in part or in full, but not exceeded.
	for name, c := range map[string][]string{
		"too much":     {"name", "Greedy", "gunnery", "100", "piloting", "51"},
		"negative":     {"name", "Greedy", "gunnery", "-5"},
		"not a number": {"name", "Greedy", "edge_tokens", "lots"},
	} {
		if got := recruit(c...); got != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", name, got)
		}
	}
	if ps, _ := store.Pilots(ctx, id, "refusalwar"); len(ps) != 3 {
		t.Fatalf("refused recruits added pilots: %d", len(ps))
	}
	if got := recruit("name", "Sybille", "gunnery", "50", "edge_tokens", "60"); got != http.StatusNoContent {
		t.Fatalf("recruit with an allocation: %d", got)
	}
	ps, _ = store.Pilots(ctx, id, "refusalwar")
	if s := ps[3]; s.Name != "Sybille" || s.TotalSP != 150 || s.SP != (pilot.SPAllocation{Gunnery: 50, EdgeTokens: 60}) || s.Unallocated() != 40 {
		t.Fatalf("a recruit that spent 110 of 150 SP: %+v", s)
	}
	if err := store.DeletePilot(ctx, id, "refusalwar", ps[3].ID); err != nil {
		t.Fatal(err)
	}
	if got := recruit("name", "Sybille", "gunnery", "90", "edge_tokens", "60"); got != http.StatusNoContent {
		t.Fatalf("recruit spending it all: %d", got)
	}
	ps, _ = store.Pilots(ctx, id, "refusalwar")
	if s := ps[3]; s.TotalSP != 150 || s.SP.Total() != 150 || s.Unallocated() != 0 || s.Gunnery() != 4 || s.EdgeTokens() != 2 {
		t.Fatalf("a recruit that spent all 150: %+v", s)
	}
	if err := store.DeletePilot(ctx, id, "refusalwar", ps[3].ID); err != nil {
		t.Fatal(err)
	}
	// With no portrait or rank chosen, a free portrait and the lowest rank are used.
	if got := recruit("name", "Sybille"); got != http.StatusNoContent {
		t.Fatalf("recruit with defaults: %d", got)
	}
	ps, _ = store.Pilots(ctx, id, "refusalwar")
	if len(ps) != 4 || ps[3].Rank != "" || !pilot.ValidPortrait(ps[3].Portrait) || ps[3].Portrait == ps[0].Portrait || ps[3].Portrait == ps[1].Portrait || ps[3].Portrait == ps[2].Portrait {
		t.Fatalf("defaults: %+v", ps[3])
	}
	// A command holds four living pilots: the button is disabled, and the server refuses a fifth.
	page = body(t, srv, "/refusalwar/manage/", cookie)
	if !strings.Contains(page, `id="recruit-open" disabled`) || !strings.Contains(page, "Your command is full: 4 of 4 pilots.") {
		t.Error("with four living pilots the button should be disabled, with a reason")
	}
	if got := recruit("name", "Fifth"); got != http.StatusBadRequest {
		t.Errorf("a fifth pilot: %d", got)
	}
	// A pilot killed in action frees a place, and the button comes back; the button stays above the memorial.
	if got := post(t, srv, "/api/pilots/fallen", url.Values{"campaign": {"refusalwar"}, "pilot": {ps[0].ID}}, cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatal(got)
	}
	page = body(t, srv, "/refusalwar/manage/", cookie)
	if strings.Contains(page, `id="recruit-open" disabled`) {
		t.Error("a killed pilot frees a place")
	}
	if i, j := strings.Index(page, `id="recruit-open"`), strings.Index(page, `class="memorial-head"`); i < 0 || j < 0 || i > j {
		t.Errorf("the Recruit button belongs above the Memorial (%d, %d)", i, j)
	}
}

// Advanced Management can set the warchest's funds; bad amounts and other origins are refused.
func TestSetWarchest(t *testing.T) {
	srv, store, cookie := startedCompany(t)
	ctx := context.Background()
	id := playthroughID("wolfpack2026")
	form := func(funds string) url.Values { return url.Values{"campaign": {"refusalwar"}, "funds": {funds}} }
	if got := post(t, srv, "/api/warchest", form("100"), cookie, "https://evil.example").StatusCode; got != http.StatusForbidden {
		t.Errorf("cross-origin: %d", got)
	}
	for _, bad := range []string{"", "-1", "12.5", "abc", "999999999999"} {
		if got := post(t, srv, "/api/warchest", form(bad), cookie, srv.URL).StatusCode; got != http.StatusBadRequest {
			t.Errorf("funds %q: %d, want 400", bad, got)
		}
	}
	if got := post(t, srv, "/api/warchest", form("1234"), cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("set funds: %d", got)
	}
	if run, _, _ := store.Run(ctx, id, "refusalwar"); run.Warchest != 1234 {
		t.Errorf("warchest %d, want 1234", run.Warchest)
	}
	page := body(t, srv, "/refusalwar/manage/", cookie)
	for _, want := range []string{`id="warchest-form"`, `value="1234"`, "1,234 SP"} {
		if !strings.Contains(page, want) {
			t.Errorf("the Manage page is missing %q", want)
		}
	}
}

func TestRecruitStatus(t *testing.T) {
	c := &content.Campaign{MaxNamedPilots: 4, RecruitCost: 300}
	for _, tc := range []struct {
		warchest, free, alive int
		can                   bool
		note                  string
	}{
		{1000, 0, 2, true, "Recruiting costs 300 SP."},
		{120, 0, 2, false, "Recruiting costs 300 SP (warchest: 120)."},
		{0, 1, 2, true, "A replacement pilot is free: recruiting costs no SP."},
		{1000, 1, 4, false, "Your command is full: 4 of 4 pilots."},
	} {
		can, note := recruitStatus(c, storage.CampaignRun{Warchest: tc.warchest, FreeRecruits: tc.free}, tc.alive)
		if can != tc.can || note != tc.note {
			t.Errorf("%+v: %v %q", tc, can, note)
		}
	}
}

// The start wizard shows each starting pilot at the campaign's first rank.
func TestStartWizardKnowsTheFirstRank(t *testing.T) {
	srv, _ := newTestServer(t)
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if p := body(t, srv, "/refusalwar/", cookie); !strings.Contains(p, `data-first-rank="MechWarrior"`) || !strings.Contains(p, "/static/pilotcard.js") {
		t.Error("the wizard knows the first rank and loads the compact pilot cards")
	}
	if (&content.Campaign{}).FirstRank() != "" {
		t.Error("a campaign with no ranks has no first rank")
	}
}

// Every page loads the shared helpers (static/dom.js) before any script of its own, since those scripts use Dom.el and Dom.commas.
func TestPagesLoadTheSharedHelpersFirst(t *testing.T) {
	srv, _, cookie := startedCompany(t)
	for _, path := range []string{"/", "/refusalwar/", "/refusalwar/manage/", "/refusalwar/scenario_001/", "/refusalwar/keywords/"} {
		p := body(t, srv, path, cookie)
		dom := strings.Index(p, `<script src="/static/dom.js"></script>`)
		first := strings.Index(p, `<script src="/static/`)
		if dom < 0 || dom != first {
			t.Errorf("%s: dom.js is the first script (at %d, first at %d)", path, dom, first)
		}
	}
}
