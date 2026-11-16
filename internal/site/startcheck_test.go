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

// hangarOf builds a hangar of standard 4/5 units from MekBay unit IDs.
func hangarOf(names ...string) HangarForce {
	h := HangarForce{Name: "Test Hangar", Timestamp: "t"}
	for i, n := range names {
		h.Units = append(h.Units, HangarUnit{ID: "u" + string(rune('a'+i)), Unit: n, Group: "Hangar", Gunnery: 4, Piloting: 5})
	}
	return h
}

// startingHangar is a hangar that meets Refusal War's starting conditions: four mechs, 5,645 BV.
func startingHangar() HangarForce {
	return hangarOf("BMKoshi_E", "BMUller_J", "BMDasher_Prime", "BMGrizzly")
}

func (f *fakeMekBay) setHangar(h HangarForce, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hangar, f.err = h, err
}

func refusalWar(t *testing.T) *Campaign {
	t.Helper()
	c, ok := (Library{Root: "../../campaigns"}).Campaign("refusalwar")
	if !ok || c.StartingBV != 6000 || c.StartingMinMechs != 4 {
		t.Fatalf("Refusal War should start with at least 4 mechs and up to 6000 BV: %+v", c)
	}
	return c
}

func TestWithCommas(t *testing.T) {
	for n, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 6000: "6,000", 12500: "12,500", 1234567: "1,234,567"} {
		if got := withCommas(n); got != want {
			t.Errorf("withCommas(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestCheckStartingHangar(t *testing.T) {
	c := refusalWar(t)

	ok := checkStartingHangar(startingHangar(), c)
	if !ok.OK || ok.Mechs != 4 || ok.BV != 997+1178+1251+2219 || len(ok.Problems) != 0 || ok.MaxBV != 6000 || ok.MinMechs != 4 || len(ok.Units) != 4 {
		t.Fatalf("a valid hangar: %+v", ok)
	}
	if ok.Units[0].Name != "Mist Lynx E" || !ok.Units[0].Mech || ok.Units[0].BV != 997 {
		t.Errorf("unit = %+v", ok.Units[0])
	}

	// Exactly at the limit is fine, and BV is base BV whatever the pilot's skills.
	atLimit := hangarOf("BMKoshi_E", "BMUller_J", "BMDasher_Prime", "BMGrizzly")
	for i := range atLimit.Units {
		atLimit.Units[i].Gunnery, atLimit.Units[i].Piloting = 0, 0 // an ace: still base BV
	}
	if r := checkStartingHangar(atLimit, c); !r.OK || r.BV != 5645 {
		t.Errorf("pilot skill must not change BV: %+v", r)
	}
	boundary := *c
	boundary.StartingBV = 5645
	if r := checkStartingHangar(startingHangar(), &boundary); !r.OK {
		t.Errorf("a hangar exactly at the BV limit is allowed: %+v", r)
	}
	boundary.StartingBV = 5644
	if r := checkStartingHangar(startingHangar(), &boundary); r.OK {
		t.Errorf("one BV over is not: %+v", r)
	}

	// Too few mechs.
	few := checkStartingHangar(hangarOf("BMKoshi_E", "BMUller_J", "BMDasher_Prime"), c)
	if few.OK || few.Mechs != 3 || len(few.Problems) != 1 || !strings.Contains(few.Problems[0], "at least 4 mechs") || !strings.Contains(few.Problems[0], "you have 3") {
		t.Errorf("too few mechs: %+v", few)
	}
	// Too much BV.
	over := checkStartingHangar(hangarOf("BMKoshi_E", "BMUller_J", "BMDasher_Prime", "BMGrizzly", "BMMadCat_H"), c)
	if over.OK || over.BV != 5645+2627 || len(over.Problems) != 1 || !strings.Contains(over.Problems[0], "8,272 BV, over the 6,000 BV") {
		t.Errorf("too much BV: %+v", over)
	}
	// Both at once, and an empty hangar.
	if both := checkStartingHangar(hangarOf("BMMadCat_H", "BMLoki_Prime", "BMGrizzly"), c); len(both.Problems) != 2 {
		t.Errorf("both problems should be reported: %+v", both.Problems)
	}
	if empty := checkStartingHangar(HangarForce{}, c); empty.OK || empty.Mechs != 0 || empty.Units == nil || empty.Problems == nil {
		t.Errorf("an empty hangar: %+v", empty)
	}
	// A unit we can't price can't be checked.
	if odd := checkStartingHangar(hangarOf("BMKoshi_E", "BMUller_J", "BMDasher_Prime", "BMNoSuchMech_X"), c); odd.OK || !strings.Contains(strings.Join(odd.Problems, " "), "BMNoSuchMech_X") || !odd.Units[3].Unknown {
		t.Errorf("an unknown unit: %+v", odd)
	}
	// A campaign with no starting conditions accepts anything.
	if r := checkStartingHangar(HangarForce{}, &Campaign{}); !r.OK {
		t.Errorf("no conditions, no problems: %+v", r)
	}
}

func TestNonMechsCountForBVButNotAsMechs(t *testing.T) {
	var tank string
	for _, u := range []string{"BMKoshi_E"} {
		_ = u
	}
	loadUnits()
	for name, u := range unitsByName {
		if u.Type == "Tank" && u.BV > 0 && u.BV < 800 {
			tank = name
			break
		}
	}
	if tank == "" {
		t.Skip("no cheap tank in the catalog")
	}
	r := checkStartingHangar(hangarOf("BMKoshi_E", "BMUller_J", "BMDasher_Prime", tank), refusalWar(t))
	if r.Mechs != 3 || r.OK || r.Units[3].Mech || r.BV <= 997+1178+1251 {
		t.Errorf("a tank adds BV but isn't a mech: %+v", r)
	}
}

func TestHangarCheckEndpoint(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, _ := newMekBayTestServer(t, fake)
	check := func(cookie *http.Cookie, link string) *http.Response {
		return get(t, srv, "/api/campaign/hangar-check?campaign=refusalwar&hangar="+url.QueryEscape(link), cookie)
	}

	if r := check(nil, goodHangar); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("no seed: %d", r.StatusCode)
	}
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	if r := check(cookie, goodHangar); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("no company: %d", r.StatusCode)
	}
	post(t, srv, "/api/company", url.Values{"company": {"Wolf's Dragoons"}}, cookie, srv.URL)
	if r := check(cookie, "https://evil.example/?instance="+goodInstance); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad link: %d", r.StatusCode)
	}
	if r := get(t, srv, "/api/campaign/hangar-check?campaign=nope&hangar="+url.QueryEscape(goodHangar), cookie); r.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown campaign: %d", r.StatusCode)
	}

	var res startCheck
	r := check(cookie, goodHangar)
	if r.StatusCode != http.StatusOK || json.NewDecoder(r.Body).Decode(&res) != nil || !res.OK || res.Mechs != 4 || res.BV != 5645 || res.MaxBV != 6000 {
		t.Fatalf("valid hangar: %d %+v", r.StatusCode, res)
	}
	if ct := r.Header.Get("Content-Type"); ct != "application/json" || r.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("headers: %q, %q", ct, r.Header.Get("Cache-Control"))
	}

	// A hangar that fails the conditions is still a successful check: the page needs the numbers.
	fake.setHangar(hangarOf("BMKoshi_E", "BMUller_J"), nil)
	res = startCheck{}
	r = check(cookie, goodHangar)
	if r.StatusCode != http.StatusOK || json.NewDecoder(r.Body).Decode(&res) != nil || res.OK || res.Mechs != 2 || len(res.Problems) != 1 {
		t.Fatalf("a failing hangar: %d %+v", r.StatusCode, res)
	}
	// MekBay trouble reaches the player.
	fake.setHangar(HangarForce{}, errors.New("MekBay has no force for your hangar link; check that the link is saved in MekBay"))
	if r := check(cookie, goodHangar); r.StatusCode != http.StatusBadGateway {
		t.Fatalf("mekbay error: %d", r.StatusCode)
	}
}

func TestStartingEnforcesTheHangarConditions(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	post(t, srv, "/api/company", url.Values{"company": {"Wolf's Dragoons"}}, cookie, srv.URL)
	id := playthroughID("wolfpack2026")
	start := func() *http.Response {
		return post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL)
	}

	for name, c := range map[string]struct {
		hangar HangarForce
		err    error
		status int
		says   string
	}{
		"too few mechs": {hangarOf("BMKoshi_E", "BMUller_J", "BMDasher_Prime"), nil, http.StatusBadRequest, "at least 4 mechs"},
		"too much BV":   {hangarOf("BMKoshi_E", "BMUller_J", "BMDasher_Prime", "BMGrizzly", "BMMadCat_H"), nil, http.StatusBadRequest, "6,000 BV starting limit"},
		"mekbay down":   {HangarForce{}, errors.New("could not reach MekBay: boom"), http.StatusBadGateway, "could not reach MekBay"},
	} {
		fake.setHangar(c.hangar, c.err)
		r := start()
		var sb strings.Builder
		buf := make([]byte, 512)
		n, _ := r.Body.Read(buf)
		sb.Write(buf[:n])
		if r.StatusCode != c.status || !strings.Contains(sb.String(), c.says) {
			t.Errorf("%s: got %d %q, want %d containing %q", name, r.StatusCode, sb.String(), c.status, c.says)
		}
		if _, started, _ := store.Run(ctx, id, "refusalwar"); started {
			t.Fatalf("%s: a failed check must not start the campaign", name)
		}
		if ps, _ := store.Pilots(ctx, id, "refusalwar"); len(ps) != 0 {
			t.Fatalf("%s: a failed check must not create pilots", name)
		}
	}

	// A hangar that meets the conditions starts the campaign.
	fake.setHangar(startingHangar(), nil)
	if r := start(); r.StatusCode != http.StatusNoContent {
		t.Fatalf("a valid hangar: %d", r.StatusCode)
	}
	if _, started, _ := store.Run(ctx, id, "refusalwar"); !started {
		t.Fatal("campaign should be started")
	}

	// The conditions are for starting: changing the link of a started campaign isn't checked again.
	fake.setHangar(hangarOf("BMKoshi_E"), nil)
	if r := post(t, srv, "/api/campaign/start", startForm("https://mekbay.com/?instance=01a12f98-8a21-7d12-b43d-52bb70872376"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("changing the hangar link: %d", r.StatusCode)
	}
}
