package site

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/mekbay"
)

// startWithUnits is the start wizard's form for a force built on the site: the mechs picked, and no hangar link.
func startWithUnits(units []string, names ...string) url.Values {
	v := startForm("", names...)
	v.Del("hangar")
	v["unit"] = units
	return v
}

// A campaign can start from mechs picked on the site: the hangar is a new force in MekBay owned by the seed's account, and checked against the starting
// conditions first.
func TestStartWithABuiltForce(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	const seed = "wolfpack2026"
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {seed}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	light := []string{"BMKoshi_E", "BMKoshi_E", "BMUller_J", "BMKoshi_E", "BMUller_J"} // two designs, picked more than once
	heavy := []string{"BMMadCat_Prime", "BMMadCat_Prime", "BMMadCat_Prime", "BMMadCat_Prime", "BMMadCat_Prime"}
	bv := func(ids []string) (n int) {
		for _, id := range ids {
			u, _ := catalog.ByName(id)
			n += u.BV
		}
		return n
	}
	if bv(light) > 10000 || bv(heavy) <= 10000 {
		t.Fatalf("the fixtures: light %d, heavy %d", bv(light), bv(heavy))
	}

	// The check endpoint checks picks as they are, without MekBay.
	check := func(units []string) (int, startCheck) {
		q := url.Values{"campaign": {"refusalwar"}, "unit": units}
		res := get(t, srv, "/api/campaign/hangar-check?"+q.Encode(), cookie)
		var out startCheck
		_ = json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	if code, out := check(light); code != http.StatusOK || !out.OK || out.Mechs != 5 || out.BV != bv(light) {
		t.Errorf("the light force passes: %d %+v", code, out)
	}
	if code, out := check(heavy); code != http.StatusOK || out.OK || len(out.Problems) == 0 {
		t.Errorf("the heavy force is over the BV limit: %d %+v", code, out)
	}
	if code, out := check(light[:3]); code != http.StatusOK || out.OK {
		t.Errorf("three mechs are too few: %d %+v", code, out)
	}
	if code, _ := check([]string{"BMNotAThing"}); code != http.StatusBadRequest {
		t.Errorf("an unknown unit is refused: %d", code)
	}

	// Starting refuses a force that fails the check, and starts nothing.
	if got := post(t, srv, "/api/campaign/start", startWithUnits(heavy, "Alrek", "Katarina"), cookie, srv.URL).StatusCode; got != http.StatusBadRequest {
		t.Errorf("over the BV limit: %d", got)
	}
	if _, ok, _ := store.Run(ctx, playthroughID(seed), "refusalwar"); ok {
		t.Fatal("nothing was started")
	}
	// A good one makes the hangar as a new force in the database, and nothing in MekBay.
	if got := post(t, srv, "/api/campaign/start", startWithUnits(light, "Alrek", "Katarina"), cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("start: %d", got)
	}
	if len(fake.reads) != 0 {
		t.Errorf("nothing is read from MekBay: %v", fake.reads)
	}
	h := runHangar(t, store, seed, "refusalwar")
	if h.Name != "Wolf's Dragoons" || len(h.Units) != 5 {
		t.Errorf("the hangar: %+v", h)
	}
	ids := map[string]bool{}
	for i, u := range h.Units {
		ids[u.ID] = true
		if u.Unit != light[i] || u.Group != "Hangar" || u.Pilot != "" || u.Gunnery != mekbay.DefaultGunnery || u.Piloting != mekbay.DefaultPiloting || u.Damaged {
			t.Errorf("each mech is pristine and uncrewed, 4/5, in the Hangar group: %+v", u)
		}
	}
	if len(ids) != 5 {
		t.Errorf("each mech has an ID of its own: %v", ids)
	}
	if run, _, _ := store.Run(ctx, playthroughID(seed), "refusalwar"); run.Warchest != 2500 {
		t.Errorf("the run: %+v", run)
	}
	if cps, _ := store.Checkpoints(ctx, playthroughID(seed), "refusalwar"); len(cps) != 1 {
		t.Errorf("and the starting checkpoint is taken: %+v", cps)
	}
	// Picked mechs can't replace a started campaign's hangar.
	if got := post(t, srv, "/api/campaign/start", startWithUnits(light, "Alrek", "Katarina"), cookie, srv.URL).StatusCode; got != http.StatusConflict {
		t.Errorf("starting again with picks: %d", got)
	}
}
