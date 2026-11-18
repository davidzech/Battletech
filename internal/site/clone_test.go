package site

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A force as MekBay saves it, with every kind of wear: lost armor and structure, a destroyed flag, a critical hit, spent ammo, heat, a torso check, and a
// named, veteran, wounded crew.
const wornForce = `{"instanceId":"src","owned":true,"name":"Mine","groups":[{"id":"g1","name":"Deployed","units":[
 {"id":"u1","unit":"BMMadCat_H","state":{"locations":{"CT":{"armor":10,"internal":4}},"destroyed":true,"heat":{"current":9,"previous":3},
  "ruleChecks":{"torso":"failed"},"crits":[{"loc":"RT","slot":2,"destroyed":true},{"loc":"LA","slot":1,"consumed":6}],
  "crew":[{"id":0,"name":"Alrek","gunnerySkill":2,"pilotingSkill":3,"hits":3,"state":1}]}},
 {"id":"u2","unit":"BMKoshi_E"}]}]}`

func TestCloneForceIsPristine(t *testing.T) {
	var src map[string]any
	if err := json.Unmarshal([]byte(wornForce), &src); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	got, err := cloneForce(src, "new-instance", now)
	if err != nil {
		t.Fatal(err)
	}
	if got["instanceId"] != "new-instance" || got["owned"] != nil {
		t.Errorf("the copy has its own instance and nothing the server fills in: %v %v", got["instanceId"], got["owned"])
	}
	if src["instanceId"] != "src" {
		t.Error("cloning must not change the source")
	}
	groups := got["groups"].([]any)
	if len(groups) != 1 || groups[0].(map[string]any)["name"] != "Deployed" {
		t.Fatalf("the arrangement is kept: %+v", groups)
	}
	for _, u := range groups[0].(map[string]any)["units"].([]any) {
		um := u.(map[string]any)
		st := um["state"].(map[string]any)
		if len(st["locations"].(map[string]any)) != 0 || st["destroyed"] != false || st["ruleChecks"] != nil {
			t.Errorf("%v: damage should be gone: %+v", um["id"], st)
		}
		if h := st["heat"].(map[string]any); h["current"] != 0 || h["previous"] != 0 {
			t.Errorf("heat: %v", h)
		}
		crew := st["crew"].([]any)
		c := crew[0].(map[string]any)
		if len(crew) != 1 || c["name"] != "" || c["gunnerySkill"] != defaultGunnery || c["pilotingSkill"] != defaultPiloting || c["hits"] != 0 {
			t.Errorf("%v: crew should be one unnamed 4/5 pilot, unwounded: %+v", um["id"], crew)
		}
		crits, _ := st["crits"].([]any)
		for _, cr := range crits {
			cm := cr.(map[string]any)
			if cm["destroyed"] == true || cm["consumed"] != nil && cm["consumed"] != 0 {
				t.Errorf("crit not cleared: %+v", cm)
			}
		}
	}
	// What a reader makes of the copy: nothing damaged, nobody named.
	b, _ := json.Marshal(map[string]any{"data": got})
	h, err := parseHangarForce(b)
	if err != nil || len(h.Units) != 2 {
		t.Fatalf("parse the copy: %v %+v", err, h)
	}
	for _, u := range h.Units {
		if u.Damaged || u.Destroyed || u.Pilot != "" || u.AmmoBins != 0 {
			t.Errorf("the copy should read as new: %+v", u)
		}
	}
}

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
	if c.Source != goodInstance || c.Target != run.HangarInstance || c.Target == goodInstance || c.OwnerUID != MekBayUID("wolfpack2026", SlotHangar) {
		t.Errorf("clone request: %+v (run %+v)", c, run)
	}
	if !run.Owned() || run.SourceURL != goodHangarURL {
		t.Errorf("the run should remember the player's link as its source: %+v", run)
	}
	// The player never sees the site's copy: neither its link nor the account that owns it.
	for _, path := range []string{"/refusalwar/", "/refusalwar/manage/", "/refusalwar/scenario_001/"} {
		p := body(t, srv, path, cookie)
		if strings.Contains(p, run.HangarInstance) || strings.Contains(p, MekBayUID("wolfpack2026", SlotHangar)) {
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
	good := edgeAbilities[0].ID
	for name, v := range map[string]url.Values{
		"none chosen":         form(),
		"too many":            form(good, edgeAbilities[1].ID),
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
