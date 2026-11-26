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

func TestStartClonesTheHangar(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar+"&sel=abc", "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	run, _, _ := store.Run(ctx, playthroughID("wolfpack2026"), "refusalwar")
	if len(fake.clones) != 1 {
		t.Fatalf("expected one clone, got %d", len(fake.clones))
	}
	c := fake.clones[0]
	if c.Source != goodInstance || c.Target != run.HangarInstance || c.Target == goodInstance || c.OwnerUID != mekbay.MekBayUID("wolfpack2026", mekbay.SlotHangar) {
		t.Errorf("clone request: %+v (run %+v)", c, run)
	}
	if !run.Owned() || run.SourceURL != goodHangarURL {
		t.Errorf("the run should remember the player's link as its source: %+v", run)
	}
	// The player never sees the site's copy: neither its link nor the account that owns it.
	for _, path := range []string{"/refusalwar/", "/refusalwar/manage/", "/refusalwar/scenario_001/"} {
		p := body(t, srv, path, cookie)
		if strings.Contains(p, run.HangarInstance) || strings.Contains(p, mekbay.MekBayUID("wolfpack2026", mekbay.SlotHangar)) {
			t.Errorf("%s leaks the site's MekBay copy or account", path)
		}
	}
	if p := body(t, srv, "/refusalwar/manage/", cookie); strings.Contains(p, `id="run-dialog"`) {
		t.Error("there is no Re-import hangar dialog any more")
	}
	// A failing clone starts nothing.
	fake2 := &fakeMekBay{hangar: startingHangar()}
	srv2, store2 := newMekBayTestServer(t, fake2)
	cookie2 := post(t, srv2, "/api/seed", url.Values{"seed": {"wolfpack2027"}}, nil, "").Cookies()[0]
	post(t, srv2, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie2, srv2.URL)
	fake2.setHangar(startingHangar(), nil)
	fake2.mu.Lock()
	fake2.cloneErr = context.DeadlineExceeded
	fake2.mu.Unlock()
	if r := post(t, srv2, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie2, srv2.URL); r.StatusCode != http.StatusBadGateway {
		t.Errorf("a failed clone: %d", r.StatusCode)
	}
	if _, ok, _ := store2.Run(ctx, playthroughID("wolfpack2027"), "refusalwar"); ok {
		t.Error("a failed clone must not start the campaign")
	}
}

// twoClones is a MekBay whose hangar clones wait for each other: neither returns until both have been asked for, so two starts sent together both get past the
// "already started" check and both make a hangar.
type twoClones struct {
	*fakeMekBay
	both sync.WaitGroup
}

func (m *twoClones) CloneHangar(ctx context.Context, req mekbay.CloneRequest) (mekbay.HangarForce, error) {
	m.both.Done()
	m.both.Wait()
	return m.fakeMekBay.CloneHangar(ctx, req)
}

// Two starts at once (a double submit) make two hangars in MekBay, and only one campaign: the hangar of the start that lost is deleted again.
func TestALostStartDeletesItsHangar(t *testing.T) {
	fake := &twoClones{fakeMekBay: &fakeMekBay{hangar: startingHangar()}}
	fake.both.Add(2)
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
	run, _, _ := store.Run(context.Background(), playthroughID("wolfpack2026"), "refusalwar")
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.clones) != 2 {
		t.Fatalf("both starts cloned the hangar: %+v", fake.clones)
	}
	for _, c := range fake.clones {
		if kept, deleted := c.Target == run.HangarInstance, slices.Contains(fake.deletes, c.Target); kept == deleted {
			t.Errorf("the campaign's hangar is kept and the other is deleted: %s kept=%v deleted=%v", c.Target, kept, deleted)
		}
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
