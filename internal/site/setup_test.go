package site

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/pilot"
)

// Starting from a link reads the player's force once and stores a clean copy of it as the campaign's hangar: the same units and IDs, every one undamaged with
// an unnamed 4/5 crew. Nothing is written to MekBay.
func TestStartImportsTheHangar(t *testing.T) {
	linked := startingHangar()
	linked.Units[0].Pilot, linked.Units[0].Gunnery, linked.Units[0].Piloting = "Anya", 3, 4
	linked.Units[1].Locations, linked.Units[1].Damaged = map[string]mekbay.LocationLoss{"LA": {Armor: 3, Internal: 1}}, true
	linked.Units[2].AmmoBins, linked.Units[2].AmmoUsed = 1, 4
	fake := &fakeMekBay{hangar: linked}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar+"&sel=abc", "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	if !slices.Equal(fake.reads, []string{goodInstance}) {
		t.Errorf("the link's force is read once: %v", fake.reads)
	}
	h := runHangar(t, store, "wolfpack2026", "refusalwar")
	if len(h.Units) != 5 || h.Units[0].ID != "ua" || h.Units[0].Unit != "BMKoshi_E" || h.Name != "Test Hangar" {
		t.Fatalf("the hangar keeps the linked force's units and IDs: %+v", h)
	}
	for _, u := range h.Units {
		if u.Pilot != "" || u.Gunnery != 4 || u.Piloting != 5 || u.Damaged || u.AmmoBins != 0 {
			t.Errorf("every unit starts clean, with an unnamed 4/5 crew: %+v", u)
		}
	}
	// The player never sees the account that owns the site's MekBay forces.
	for _, path := range []string{"/refusalwar/", "/refusalwar/manage/", "/refusalwar/scenario_001/"} {
		if p := body(t, srv, path, cookie); strings.Contains(p, mekbay.MekBayUID("wolfpack2026", mekbay.SlotHangar)) {
			t.Errorf("%s leaks the site's MekBay account", path)
		}
	}
	if p := body(t, srv, "/refusalwar/manage/", cookie); strings.Contains(p, `id="run-dialog"`) {
		t.Error("there is no Re-import hangar dialog any more")
	}
	// A link MekBay can't read starts nothing.
	fake2 := &fakeMekBay{hangar: startingHangar(), err: context.DeadlineExceeded}
	srv2, store2 := newMekBayTestServer(t, fake2)
	cookie2 := post(t, srv2, "/api/seed", url.Values{"seed": {"wolfpack2027"}}, nil, "").Cookies()[0]
	post(t, srv2, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie2, srv2.URL)
	if r := post(t, srv2, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie2, srv2.URL); r.StatusCode != http.StatusBadGateway {
		t.Errorf("a link that can't be read: %d", r.StatusCode)
	}
	if _, ok, _ := store2.Run(ctx, playthroughID("wolfpack2027"), "refusalwar"); ok {
		t.Error("a link that can't be read must not start the campaign")
	}
}

// Two starts at once (a double submit) make one campaign: the other is told it has been started.
func TestTwoStartsAtOnceStartOnce(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	codes := make([]int, 2)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("POST", srv.URL+"/api/campaign/start", strings.NewReader(startForm(goodHangar, "Alrek", "Katarina").Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", srv.URL)
			req.AddCookie(cookie)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			res.Body.Close()
			codes[i] = res.StatusCode
		}()
	}
	wg.Wait()
	slices.Sort(codes)
	if !slices.Equal(codes, []int{http.StatusNoContent, http.StatusConflict}) {
		t.Fatalf("one start goes through and the other is told the campaign is started: %v", codes)
	}
	if ps, _ := store.Pilots(context.Background(), playthroughID("wolfpack2026"), "refusalwar"); len(ps) != 2 {
		t.Errorf("one start's pilots: %+v", ps)
	}
}

// A starting pilot whose allocation reaches an Edge abilities level must choose that many abilities in the wizard, and starts with them.
func TestStartChoosesEdgeAbilities(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	form := func(abilities ...string) url.Values {
		v := startForm(goodHangar, "Alrek", "Katarina")
		v.Set("pilot0_gunnery", "30")
		v.Set("pilot0_edge_abilities", "60") // Edge abilities level 1
		v["pilot0_ability"] = abilities
		return v
	}
	good := pilot.AbilityCatalog[0].ID
	for name, v := range map[string]url.Values{
		"none chosen":         form(),
		"too many":            form(good, pilot.AbilityCatalog[1].ID),
		"an unknown one":      form("no-such-ability"),
		"for a level-0 pilot": func() url.Values { v := form(good); v["pilot1_ability"] = []string{good}; return v }(),
	} {
		if r := post(t, srv, "/api/campaign/start", v, cookie, srv.URL); r.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d", name, r.StatusCode)
		}
	}
	if _, ok, _ := store.Run(ctx, playthroughID("wolfpack2026"), "refusalwar"); ok {
		t.Fatal("a refused start must not start the campaign")
	}
	if r := post(t, srv, "/api/campaign/start", form(good), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("a good start: %d", r.StatusCode)
	}
	pilots, _ := store.Pilots(ctx, playthroughID("wolfpack2026"), "refusalwar")
	for _, p := range pilots {
		if p.Name == "Alrek" && (len(p.Learned) != 1 || p.Learned[0] != good || p.PendingAbilities() != 0) {
			t.Errorf("Alrek should start with %s and nothing pending: %+v", good, p.Learned)
		}
		if p.Name == "Katarina" && len(p.Learned) != 0 {
			t.Errorf("Katarina has no Edge abilities level: %+v", p.Learned)
		}
	}
}
