package site

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestLadders(t *testing.T) {
	for _, c := range []struct {
		l         ladder
		sp, level int
	}{
		// Gunnery starts at 4 and improves one step at a time as cumulative SP is reached.
		{gunneryLadder, 0, 4},
		{gunneryLadder, 299, 4},
		{gunneryLadder, 300, 3},
		{gunneryLadder, 699, 3},
		{gunneryLadder, 700, 2},
		{gunneryLadder, 1200, 1},
		{gunneryLadder, 2200, 0},
		{gunneryLadder, 99999, 0}, // the top of the ladder holds
		// Piloting starts at 5 and stops at 1.
		{pilotingLadder, 99, 5},
		{pilotingLadder, 100, 4},
		{pilotingLadder, 200, 3},
		{pilotingLadder, 700, 2},
		{pilotingLadder, 1200, 1},
		{pilotingLadder, 5000, 1},
		// Edge tokens start at 1.
		{edgeTokensLadder, 59, 1},
		{edgeTokensLadder, 60, 2},
		{edgeTokensLadder, 120, 3},
		{edgeTokensLadder, 560, 7},
		{edgeTokensLadder, 1100, 10},
		// Edge abilities start at none.
		{edgeAbilitiesLadder, 59, 0},
		{edgeAbilitiesLadder, 60, 1},
		{edgeAbilitiesLadder, 180, 2},
		{edgeAbilitiesLadder, 900, 5},
		{edgeAbilitiesLadder, 5000, 5},
	} {
		if got := c.l.at(c.sp); got.Level != c.level {
			t.Errorf("%s at %d SP = level %d, want level %d", c.l.Name, c.sp, got.Level, c.level)
		}
	}
	if n, ok := gunneryLadder.next(0); !ok || n.SP != 300 || n.Level != 3 {
		t.Errorf("next gunnery step from 0 = %+v, %v", n, ok)
	}
	if n, ok := gunneryLadder.next(300); !ok || n.SP != 700 {
		t.Errorf("next gunnery step from 300 = %+v, %v", n, ok)
	}
	if _, ok := gunneryLadder.next(2200); ok {
		t.Error("there is nothing above Gunnery 0")
	}
	// Every ladder's SP costs must rise, or a cheaper step could be skipped.
	for _, l := range []ladder{gunneryLadder, pilotingLadder, edgeTokensLadder, edgeAbilitiesLadder} {
		for i := 1; i < len(l.Rows); i++ {
			if l.Rows[i].SP <= l.Rows[i-1].SP {
				t.Errorf("%s: row %d (%+v) must cost more than the one before", l.Name, i, l.Rows[i])
			}
		}
	}
}

// TestKirbyExample replays the worked example in the book (Hot Spots: Draconis Reach, p. 34): Kirby puts 120 SP into
// Edge tokens and 30 into Edge abilities, then later 60 more into abilities.
func TestKirbyExample(t *testing.T) {
	kirby := NamedPilot{Name: "Kirby", SP: SPAllocation{EdgeTokens: 120, EdgeAbilities: 30}}
	if kirby.EdgeTokens() != 3 || kirby.EdgeAbilities() != 0 || kirby.Earned() != 150 {
		t.Fatalf("Kirby after 150 SP: tokens %d, abilities %d, earned %d (the book says 3, 0, 150)", kirby.EdgeTokens(), kirby.EdgeAbilities(), kirby.Earned())
	}
	kirby.SP.EdgeAbilities += 60
	if kirby.SP.EdgeAbilities != 90 || kirby.EdgeAbilities() != 1 {
		t.Fatalf("Kirby after 60 more: abilities SP %d, abilities %d (the book says 90, 1)", kirby.SP.EdgeAbilities, kirby.EdgeAbilities())
	}
	// A brand new pilot is a standard 4/5 pilot with one Edge token and no abilities.
	if p := (NamedPilot{}); p.Gunnery() != 4 || p.Piloting() != 5 || p.EdgeTokens() != 1 || p.EdgeAbilities() != 0 {
		t.Errorf("new pilot = %d/%d, edge %d, abilities %d", p.Gunnery(), p.Piloting(), p.EdgeTokens(), p.EdgeAbilities())
	}
}

func TestPilotView(t *testing.T) {
	views, alive := pilotViews([]NamedPilot{
		{ID: "a", Name: "Alrek", SP: SPAllocation{Gunnery: 300, Piloting: 50}},
		{ID: "b", Name: "Katarina", Fallen: true},
	})
	if alive != 1 || len(views) != 2 {
		t.Fatalf("alive %d of %d", alive, len(views))
	}
	a := views[0]
	if a.Gunnery != 3 || a.Piloting != 5 || a.Earned != 350 || !views[1].Fallen {
		t.Errorf("view = %+v", a)
	}
	g, p := a.Columns[0], a.Columns[1]
	if g.Name != "Gunnery" || g.SP != 300 || !g.HasNext || g.NextAt != 700 || g.NextLvl != 2 {
		t.Errorf("gunnery column = %+v", g)
	}
	if p.SP != 50 || p.NextAt != 100 || p.NextLvl != 4 {
		t.Errorf("piloting column = %+v", p)
	}
}

func TestParsePilotName(t *testing.T) {
	for _, good := range []string{"Alrek", "Star Captain Alrek", "Ulric d'Aleshire"} {
		if got, err := parsePilotName(good); err != nil || got != good {
			t.Errorf("parsePilotName(%q) = %q, %v", good, got, err)
		}
	}
	for _, bad := range []string{"", "  ", "<b>", strings.Repeat("a", 31), "a\x00b"} {
		if _, err := parsePilotName(bad); err == nil {
			t.Errorf("parsePilotName(%q) should fail", bad)
		}
	}
}

func TestRefusalWarStartsWithTwoNamedPilots(t *testing.T) {
	c, ok := (Library{Root: "../../campaigns"}).Campaign("refusalwar")
	if !ok || c.StartingNamedPilots != 2 || c.StartingPilotSP != 150 || c.StartingBV != 6000 || c.StartingMinMechs != 4 {
		t.Fatalf("Refusal War starts with two named pilots (150 SP each) and a 6000 BV force: %+v", c)
	}
}

// pilotStore exercises the named pilot queries; it is run for both databases from storeContract.
func pilotStore(t *testing.T, s *Store, id string) {
	t.Helper()
	ctx := context.Background()
	if ps, err := s.Pilots(ctx, id, "refusalwar"); err != nil || len(ps) != 0 {
		t.Fatalf("no pilots yet: %v %v", ps, err)
	}
	a, err := s.AddPilot(ctx, id, "refusalwar", "Alrek", "", SPAllocation{Gunnery: 150}, 2)
	if err != nil || a.ID == "" || a.Name != "Alrek" {
		t.Fatalf("add pilot: %+v, %v", a, err)
	}
	b, err := s.AddPilot(ctx, id, "refusalwar", "Katarina", "", SPAllocation{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPilot(ctx, id, "refusalwar", "Third", "", SPAllocation{}, 2); !errors.Is(err, ErrPilotLimit) {
		t.Fatalf("the limit should refuse a third pilot, got %v", err)
	}
	// Another campaign has its own pilots.
	if _, err := s.AddPilot(ctx, id, "another", "Elsewhere", "", SPAllocation{}, 2); err != nil {
		t.Fatalf("pilots belong to a campaign: %v", err)
	}
	ps, err := s.Pilots(ctx, id, "refusalwar")
	if err != nil || len(ps) != 2 || ps[0].ID != a.ID || ps[1].ID != b.ID || ps[0].SP.Gunnery != 150 || ps[0].Fallen {
		t.Fatalf("pilots = %+v, %v", ps, err)
	}

	// SP adds to the columns.
	if err := s.AwardSP(ctx, id, "refusalwar", a.ID, SPAllocation{Gunnery: 150, EdgeTokens: 60}); err != nil {
		t.Fatal(err)
	}
	ps, _ = s.Pilots(ctx, id, "refusalwar")
	if ps[0].SP != (SPAllocation{Gunnery: 300, EdgeTokens: 60}) || ps[0].Gunnery() != 3 || ps[0].EdgeTokens() != 2 {
		t.Fatalf("after award: %+v", ps[0])
	}
	if err := s.AwardSP(ctx, id, "refusalwar", "no-such-pilot", SPAllocation{Gunnery: 1}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown pilot: %v", err)
	}
	if err := s.AwardSP(ctx, id, "another", a.ID, SPAllocation{Gunnery: 1}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("another campaign's pilot: %v", err)
	}

	// A fallen pilot frees their place, keeps their record, and can no longer earn SP.
	if err := s.MarkFallen(ctx, id, "refusalwar", b.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkFallen(ctx, id, "refusalwar", b.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("already fallen: %v", err)
	}
	if err := s.AwardSP(ctx, id, "refusalwar", b.ID, SPAllocation{Gunnery: 1}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("the fallen earn nothing: %v", err)
	}
	if _, err := s.AddPilot(ctx, id, "refusalwar", "Replacement", "", SPAllocation{}, 2); err != nil {
		t.Fatalf("a fallen pilot should free a place: %v", err)
	}
	if ps, _ := s.Pilots(ctx, id, "refusalwar"); len(ps) != 3 || !ps[1].Fallen {
		t.Fatalf("the fallen pilot should stay on record: %+v", ps)
	}

	// Starting a campaign creates its run and pilots together, and only once.
	start := []NewPilot{{Name: "Alrek", SP: SPAllocation{Gunnery: 90, EdgeTokens: 60}}, {Name: "Katarina", SP: SPAllocation{Piloting: 100, EdgeTokens: 50}}}
	hangar := Hangar{URL: goodHangarURL, Instance: goodInstance}
	if err := s.StartRunWithPilots(ctx, id, "wizard", hangar, start); err != nil {
		t.Fatal(err)
	}
	if r, ok, _ := s.Run(ctx, id, "wizard"); !ok || r.HangarInstance != goodInstance {
		t.Fatalf("run not started: %+v", r)
	}
	if ps, _ := s.Pilots(ctx, id, "wizard"); len(ps) != 2 || ps[0].Name != "Alrek" || ps[0].SP != start[0].SP || ps[1].SP != start[1].SP || ps[1].Piloting() != 4 {
		t.Fatalf("starting pilots = %+v", ps)
	}
	if err := s.StartRunWithPilots(ctx, id, "wizard", hangar, start); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("starting twice should be refused, got %v", err)
	}
	if ps, _ := s.Pilots(ctx, id, "wizard"); len(ps) != 2 {
		t.Fatalf("a refused start must not add pilots: %+v", ps)
	}
	if err := s.StartRunWithPilots(ctx, "no-such-playthrough", "wizard", hangar, start); err == nil {
		t.Fatal("a run needs a playthrough")
	}
}

// startForm is what the start wizard posts: the hangar link plus, for each name, a pilot who allocates all 150 of
// their starting SP (90 to Gunnery, 60 to Edge tokens).
func startForm(hangar string, names ...string) url.Values {
	v := url.Values{"campaign": {"refusalwar"}, "hangar": {hangar}}
	for i, n := range names {
		p := "pilot" + strconv.Itoa(i) + "_"
		v.Set(p+"name", n)
		v.Set(p+"gunnery", "90")
		v.Set(p+"edge_tokens", "60")
	}
	return v
}

// startedCompany sets up a test server with a company that has started Refusal War.
func startedCompany(t *testing.T) (*httptest.Server, *Store, *http.Cookie) {
	t.Helper()
	srv, store := newTestServer(t)
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	if r := post(t, srv, "/api/company", url.Values{"company": {"Wolf's Dragoons"}}, cookie, srv.URL); r.StatusCode != http.StatusNoContent {
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
	post(t, srv, "/api/company", url.Values{"company": {"Wolf's Dragoons"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/pilots", pilots("name", "Alrek"), cookie, srv.URL); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("campaign not started: %d", r.StatusCode)
	}
	if c := body(t, srv, "/refusalwar/", cookie); strings.Contains(c, `id="pilots"`) {
		t.Fatal("the pilots panel belongs to a started campaign")
	}
	id := playthroughID("wolfpack2026")
	// A campaign started without pilots (before they were part of starting one) has to name them afterwards.
	if err := store.StartRun(ctx, id, "refusalwar", Hangar{URL: goodHangarURL, Instance: goodInstance}); err != nil {
		t.Fatal(err)
	}

	// A started campaign asks for its two starting pilots.
	if c := body(t, srv, "/refusalwar/", cookie); !strings.Contains(c, `id="pilots"`) || !strings.Contains(c, "needs 2 named pilots") || !strings.Contains(c, "Name 2 more") || !strings.Contains(c, `id="pilot-add"`) {
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
	c := body(t, srv, "/refusalwar/", cookie)
	if strings.Contains(c, "needs 2 named pilots") || strings.Contains(c, `id="pilot-add"`) || !strings.Contains(c, "Alrek") || !strings.Contains(c, "Sybille") {
		t.Fatalf("with four pilots there is nothing left to ask for: %.3000s", c)
	}
	ps, _ := store.Pilots(ctx, id, "refusalwar")
	alrek := ps[0]
	if alrek.Name != "Alrek" || alrek.Earned() != 0 || !strings.Contains(c, `data-pilot="`+alrek.ID+`"`) {
		t.Fatalf("alrek = %+v", alrek)
	}

	// Awarding SP.
	award := func(pilot string, kv ...string) int {
		v := pilots("pilot", pilot)
		for i := 0; i+1 < len(kv); i += 2 {
			v.Set(kv[i], kv[i+1])
		}
		return post(t, srv, "/api/pilots/sp", v, cookie, srv.URL).StatusCode
	}
	for name, c := range map[string]struct {
		status int
		kv     []string
	}{
		"nothing":       {http.StatusBadRequest, nil},
		"all zero":      {http.StatusBadRequest, []string{"gunnery", "0"}},
		"negative":      {http.StatusBadRequest, []string{"gunnery", "-5"}},
		"not a number":  {http.StatusBadRequest, []string{"gunnery", "lots"}},
		"absurd":        {http.StatusBadRequest, []string{"gunnery", "99999999"}},
		"one bad field": {http.StatusBadRequest, []string{"gunnery", "100", "piloting", "x"}},
	} {
		if got := award(alrek.ID, c.kv...); got != c.status {
			t.Errorf("award %s: got %d, want %d", name, got, c.status)
		}
	}
	if got := award("no-such-pilot", "gunnery", "100"); got != http.StatusNotFound {
		t.Errorf("unknown pilot: %d", got)
	}
	if ps, _ := store.Pilots(ctx, id, "refusalwar"); ps[0].Earned() != 0 {
		t.Fatalf("refused awards changed the pilot: %+v", ps[0])
	}
	// The trial pays 150 to 900 SP, so there is no per-award cap below the typo guard.
	if got := award(alrek.ID, "gunnery", "300", "edge_tokens", "60", "edge_abilities", ""); got != http.StatusNoContent {
		t.Fatalf("award: %d", got)
	}
	ps, _ = store.Pilots(ctx, id, "refusalwar")
	if ps[0].SP != (SPAllocation{Gunnery: 300, EdgeTokens: 60}) {
		t.Fatalf("allocation = %+v", ps[0].SP)
	}
	c = body(t, srv, "/refusalwar/", cookie)
	if !strings.Contains(c, "<b>3</b>") || !strings.Contains(c, "SP earned <b>360</b>") || !strings.Contains(c, "next level 2 at 700 SP") {
		t.Fatalf("the page should show the new levels (gunnery 3, edge 2): %.4000s", c)
	}

	// A fallen pilot is marked as such, can't earn more, and frees their place.
	if r := post(t, srv, "/api/pilots/fallen", pilots("pilot", ps[1].ID), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("fallen: %d", r.StatusCode)
	}
	if r := post(t, srv, "/api/pilots/fallen", pilots("pilot", ps[1].ID), cookie, srv.URL); r.StatusCode != http.StatusNotFound {
		t.Fatalf("already fallen: %d", r.StatusCode)
	}
	if got := award(ps[1].ID, "gunnery", "100"); got != http.StatusNotFound {
		t.Errorf("award to the fallen: %d", got)
	}
	if c := body(t, srv, "/refusalwar/", cookie); !strings.Contains(c, `class="pilot fallen"`) || !strings.Contains(c, "Fallen") || !strings.Contains(c, `id="pilot-add"`) {
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
	post(t, srv, "/api/company", url.Values{"company": {"Wolf's Dragoons"}}, cookie, srv.URL)
	id := playthroughID("wolfpack2026")

	// The wizard page carries what the script needs: the pilot count, the SP budget and the ladders.
	page := body(t, srv, "/refusalwar/", cookie)
	for _, want := range []string{`id="start-wizard"`, `data-pilots="2"`, `data-sp="150"`, `data-ladders="[{&#34;key&#34;:&#34;gunnery&#34;`, `id="wiz-progress"`, `id="wizard-open"`} {
		if !strings.Contains(page, want) {
			t.Errorf("wizard page is missing %s", want)
		}
	}
	if strings.Contains(page, `id="run-dialog"`) {
		t.Error("an unstarted campaign has no change-hangar dialog")
	}
	// The starting rules are a short bulleted list, not one dense sentence.
	for _, want := range []string{`class="start-rules"`, `<li>Your hangar holds <b>at least 4 mechs</b>, totalling <b>up to 6,000 BV</b> (base BV, whatever the pilots&rsquo; skills).</li>`, `<li>You create <b>2 named pilots</b>, each allocating <b>150 SP</b>.</li>`, `<li>You link this campaign&rsquo;s MekBay hangar`} {
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
	if len(ps) != 2 || ps[0].Name != "Alrek" || ps[0].SP != (SPAllocation{Gunnery: 90, EdgeTokens: 60}) ||
		ps[1].Name != "Katarina" || ps[1].SP != (SPAllocation{Piloting: 100, EdgeAbilities: 50}) {
		t.Fatalf("pilots = %+v", ps)
	}
	if ps[0].EdgeTokens() != 2 || ps[1].Piloting() != 4 || ps[1].EdgeAbilities() != 0 {
		t.Errorf("levels follow the ladders: %+v", ps)
	}
	if c := body(t, srv, "/refusalwar/", cookie); !strings.Contains(c, "Katarina") || strings.Contains(c, "needs 2 named pilots") || strings.Contains(c, `id="start-wizard"`) || !strings.Contains(c, `id="run-dialog"`) {
		t.Fatalf("a started campaign shows its pilots and the change-link dialog: %.3000s", c)
	}

	// Submitting the wizard again only changes the hangar link: no duplicate pilots.
	other := "https://mekbay.com/?instance=01a12f98-8a21-7d12-b43d-52bb70872376"
	if r := post(t, srv, "/api/campaign/start", startForm(other, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("second submit: %d", r.StatusCode)
	}
	if ps, _ := store.Pilots(ctx, id, "refusalwar"); len(ps) != 2 {
		t.Fatalf("a second submit must not duplicate pilots: %+v", ps)
	}
	if run, _, _ := store.Run(ctx, id, "refusalwar"); run.HangarInstance != "01a12f98-8a21-7d12-b43d-52bb70872376" {
		t.Fatalf("the second submit should change the hangar: %+v", run)
	}
}

func TestStartedCompanyHelper(t *testing.T) {
	_, store, _ := startedCompany(t)
	if run, ok, _ := store.Run(context.Background(), playthroughID("wolfpack2026"), "refusalwar"); !ok || !run.Started() {
		t.Fatal("helper should start the campaign")
	}
}

func TestPortraits(t *testing.T) {
	all := portraits()
	if len(all) < 70 {
		t.Fatalf("only %d portraits embedded", len(all))
	}
	for _, p := range all {
		if !validPortrait(p.File) || p.Group == "" || p.Label == "" {
			t.Errorf("bad portrait %+v", p)
		}
	}
	if validPortrait("../x.png") || validPortrait("") || validPortrait("nope.png") {
		t.Error("invalid names accepted")
	}
	// Legacy pilots (no stored portrait) get a stable one; a picked one wins.
	if first := portraitOf("abc", ""); first != portraitOf("abc", "") || !validPortrait(first) {
		t.Error("fallback portrait not stable or invalid")
	}
	if portraitOf("abc", all[3].File) != all[3].File {
		t.Error("picked portrait ignored")
	}
	// A random pick avoids the taken ones.
	taken := map[string]bool{}
	for _, p := range all[1:] {
		taken[p.File] = true
	}
	if randomPortrait(taken) != all[0].File {
		t.Error("random pick used a taken portrait")
	}
}

// portraitStore checks the portrait rules on a store (run for both databases from storeContract, in a campaign of its own).
func portraitStore(t *testing.T, s *Store, id string) {
	t.Helper()
	ctx := context.Background()
	pa, pb := portraits()[0].File, portraits()[1].File
	a, err := s.AddPilot(ctx, id, "portraitcamp", "Alrek", pa, SPAllocation{}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPilot(ctx, id, "portraitcamp", "Katarina", pa, SPAllocation{}, 4); !errors.Is(err, ErrPortraitTaken) {
		t.Fatalf("same portrait: %v", err)
	}
	b, err := s.AddPilot(ctx, id, "portraitcamp", "Katarina", pb, SPAllocation{}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPilotPortrait(ctx, id, "portraitcamp", b.ID, pa); !errors.Is(err, ErrPortraitTaken) {
		t.Fatalf("change to a taken one: %v", err)
	}
	pc := portraits()[2].File
	if err := s.SetPilotPortrait(ctx, id, "portraitcamp", b.ID, pc); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPilotPortrait(ctx, id, "portraitcamp", "nobody", portraits()[3].File); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown pilot: %v", err)
	}
	// A fallen pilot frees their portrait.
	if err := s.MarkFallen(ctx, id, "portraitcamp", a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPilot(ctx, id, "portraitcamp", "Heir", pa, SPAllocation{}, 4); err != nil {
		t.Fatalf("fallen pilot's portrait should be free: %v", err)
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
	if page := body(t, srv, "/refusalwar/", cookie); !strings.Contains(page, `id="delete-dialog"`) {
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
