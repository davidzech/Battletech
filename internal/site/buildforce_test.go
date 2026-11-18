package site

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"
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
			u, _ := unitByName(id)
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

	// Starting refuses a force that fails the check, and makes nothing in MekBay.
	if got := post(t, srv, "/api/campaign/start", startWithUnits(heavy, "Alrek", "Katarina"), cookie, srv.URL).StatusCode; got != http.StatusBadRequest {
		t.Errorf("over the BV limit: %d", got)
	}
	if len(fake.created) != 0 {
		t.Fatalf("nothing was made: %+v", fake.created)
	}
	// A good one makes the hangar as a new force, and the run points at it.
	if got := post(t, srv, "/api/campaign/start", startWithUnits(light, "Alrek", "Katarina"), cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("start: %d", got)
	}
	if len(fake.created) != 1 {
		t.Fatalf("one force made: %+v", fake.created)
	}
	made := fake.created[0]
	if made.Owner != MekBayUID(seed, SlotHangar) || made.Draft.Name != "Wolf's Dragoons" || len(made.Draft.Groups) != 1 || made.Draft.Groups[0].Name != "Hangar" || len(made.Draft.Groups[0].Units) != 5 {
		t.Errorf("the force: %+v", made)
	}
	for _, u := range made.Draft.Groups[0].Units {
		if u.Pilot != "" || u.Gunnery != defaultGunnery || u.Piloting != defaultPiloting || u.BV == 0 {
			t.Errorf("each mech is uncrewed, 4/5, with its BV: %+v", u)
		}
	}
	run, _, _ := store.Run(ctx, playthroughID(seed), "refusalwar")
	if run.HangarInstance != made.Instance || !run.Owned() || run.Warchest != 5000 {
		t.Errorf("the run's hangar is the new force, and the site owns it: %+v", run)
	}
	if h, _, ok, _ := store.CachedHangar(ctx, playthroughID(seed), "refusalwar", made.Instance); !ok || len(h.Units) != 5 {
		t.Errorf("the new hangar is cached: %v %+v", ok, h)
	}
	if cps, _ := store.Checkpoints(ctx, playthroughID(seed), "refusalwar"); len(cps) != 1 {
		t.Errorf("and the starting checkpoint is taken: %+v", cps)
	}
	// Picked mechs can't replace a started campaign's hangar.
	if got := post(t, srv, "/api/campaign/start", startWithUnits(light, "Alrek", "Katarina"), cookie, srv.URL).StatusCode; got != http.StatusConflict {
		t.Errorf("starting again with picks: %d", got)
	}
}

// TestCreateForceLive makes a force on the real mekbay.com with a throwaway account and reads it back. It runs only when MEKBAY_LIVE_BATTLE is set.
func TestCreateForceLive(t *testing.T) {
	if os.Getenv("MEKBAY_LIVE_BATTLE") == "" {
		t.Skip("set MEKBAY_LIVE_BATTLE=1 to run against mekbay.com")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cl := &MekBayClient{}
	id := newID()
	got, err := cl.CreateForce(ctx, newID(), id, ForceDraft{Name: "Live built", Groups: []GroupDraft{{Name: "Hangar", Units: []UnitDraft{{Unit: "BMKoshi_E", Gunnery: 4, Piloting: 5}, {Unit: "BMKoshi_E", Gunnery: 4, Piloting: 5}}}}})
	if err != nil || len(got.Units) != 2 {
		t.Fatalf("create: %+v %v", got, err)
	}
	h, err := cl.ReadHangar(ctx, id)
	if err != nil || len(h.Units) != 2 || h.Units[0].Unit != "BMKoshi_E" || h.Units[0].ID == h.Units[1].ID || h.Units[0].Gunnery != 4 || h.Units[0].Pilot != "" {
		t.Errorf("read back: %+v %v", h, err)
	}
}
