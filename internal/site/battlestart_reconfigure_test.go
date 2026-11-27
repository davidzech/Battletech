package site

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/storage"
)

// The Deploy wizard can reconfigure hangar omnimechs on its review, typically to bring the force under the mission's BV limit: the plan counts the variants,
// costs what the Manage page's Reconfigure costs, and Start battle pays for the swaps and makes them in the hangar before it builds the battle.
func TestStartBattleReconfiguresHangarMechs(t *testing.T) {
	const seed = "wolfpack2026"
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {seed}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	id := playthroughID(seed)
	if _, err := store.SetScenarioCompleted(ctx, id, "refusalwar", "scenario_000", true); err != nil {
		t.Fatal(err)
	}
	setFunds := func(sp int) {
		if _, err := store.DB().Exec(`UPDATE campaign_runs SET warchest = ?`, sp); err != nil {
			t.Fatal(err)
		}
	}
	funds := func() int { r, _, _ := store.Run(ctx, id, "refusalwar"); return r.Warchest }
	setFunds(1000)
	// Two Timber Wolf Primes (2,737 BV each) and two Fire Moth Primes (1,251): 7,976 BV, over Scenario 001's 7,500. The Timber Wolf B is 2,224 BV and costs
	// 38 SP (half of 75 tons, rounded up; no BV gained), which brings the force to 7,463. ud is a Timber Wolf with spent ammunition.
	h := hangarOf("BMMadCat_Prime", "BMMadCat_Prime", "BMDasher_Prime", "BMDasher_Prime", "BMMadCat_Prime")
	h.Units[4].Damaged, h.Units[4].AmmoBins, h.Units[4].AmmoUsed = true, 1, 3
	fake.setHangar(h, nil)
	form := func(kv ...string) url.Values {
		v := url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_001"}, "players": {"1"}}
		for _, u := range []string{"ua", "ub", "uc", "ud"} {
			v.Add("p1_units", u)
		}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Add(kv[i], kv[i+1])
		}
		return v
	}
	plan := func(v url.Values) (int, string, planResult) {
		res := post(t, srv, "/api/deploy/plan", v, cookie, srv.URL)
		b, _ := io.ReadAll(res.Body)
		var out planResult
		if res.StatusCode == http.StatusOK {
			if err := json.Unmarshal(b, &out); err != nil {
				t.Fatalf("%v: %s", err, b)
			}
		}
		return res.StatusCode, strings.TrimSpace(string(b)), out
	}
	swapB := []string{"variant_unit", "ua", "variant_to", "BMMadCat_B"}

	// As it stands the force is over the limit; with the B it fits, and the plan says what reconfiguring costs.
	if code, msg, _ := plan(form()); code != http.StatusBadRequest || !strings.Contains(msg, "over this scenario's limit") {
		t.Errorf("over the limit: %d %q", code, msg)
	}
	if code, msg, res := plan(form(swapB...)); code != http.StatusOK || res.TotalBV != 7463 || res.SwapCost != 38 || res.Players[0].Units[0].Model != "B" {
		t.Errorf("with the B: %d %q %+v", code, msg, res)
	}
	// The Manage page's rules: an omnimech, fully repaired and rearmed, to a variant of its own chassis, deployed, and paid for from the warchest.
	for name, c := range map[string]struct {
		v    url.Values
		want int
		msg  string
	}{
		"spent ammunition": {form("variant_unit", "ue", "variant_to", "BMMadCat_B"), http.StatusBadRequest, "fully repaired and rearmed"},
		"another chassis":  {form("variant_unit", "ua", "variant_to", "BMDasher_A"), http.StatusBadRequest, "can't be reconfigured to that variant"},
		"twice":            {form("variant_unit", "ua", "variant_to", "BMMadCat_B", "variant_unit", "ua", "variant_to", "BMMadCat_C"), http.StatusBadRequest, "reconfigured twice"},
		"unpaired":         {form("variant_unit", "ua"), http.StatusBadRequest, "needs a variant"},
	} {
		code, msg, _ := plan(c.v)
		if code != c.want || !strings.Contains(msg, c.msg) {
			t.Errorf("%s: %d %q", name, code, msg)
		}
	}
	// A clean Timber Wolf that isn't being deployed can't be reconfigured on the way.
	notTaken := url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_001"}, "players": {"1"}, "p1_units": {"uc", "ud"}, "variant_unit": {"ua"}, "variant_to": {"BMMadCat_B"}}
	if code, msg, _ := plan(notTaken); code != http.StatusBadRequest || !strings.Contains(msg, "isn't being deployed") {
		t.Errorf("a mech not deployed: %d %q", code, msg)
	}
	setFunds(37)
	if code, msg, _ := plan(form(swapB...)); code != http.StatusConflict || !strings.Contains(msg, "Not enough in the warchest") {
		t.Errorf("too little in the warchest: %d %q", code, msg)
	}
	setFunds(1000)
	if len(fake.swaps) != 0 || len(fake.battles) != 0 {
		t.Fatalf("checking a plan reached MekBay: %+v %d", fake.swaps, len(fake.battles))
	}

	// A swap MekBay fails is refunded, and the battle isn't built.
	fake.serviceErr = context.DeadlineExceeded
	if got := post(t, srv, "/api/battle/start", form(swapB...), cookie, srv.URL).StatusCode; got != http.StatusBadGateway || funds() != 1000 || len(fake.battles) != 0 {
		t.Errorf("MekBay failure: %d, funds %d, battles %d", got, funds(), len(fake.battles))
	}
	if _, ok, _ := store.MekBayOperation(ctx, id, "refusalwar", "scenario_001"); ok {
		t.Fatal("a failed swap started the scenario")
	}
	fake.serviceErr = nil

	// Start battle pays for the swap, makes it in the hangar under the player's account, logs it, and then builds the battle.
	fake.swaps = nil
	readBattle(t, post(t, srv, "/api/battle/start", form(swapB...), cookie, srv.URL))
	if len(fake.swaps) != 1 || fake.swaps[0].UnitID != "ua" || fake.swaps[0].To != "BMMadCat_B" || fake.swaps[0].OwnerUID != mekbay.MekBayUID(seed, mekbay.SlotHangar) {
		t.Errorf("MekBay was asked: %+v", fake.swaps)
	}
	if funds() != 1000-38 || len(fake.battles) != 1 {
		t.Errorf("funds %d (want %d), battles %d", funds(), 1000-38, len(fake.battles))
	}
	entries, err := store.History(ctx, id, "refusalwar")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Kind == storage.HistoryHangar && e.SP == -38 && e.Text == "Reconfigured Timber Wolf Prime to B for 38 SP before Pyre Light." {
			found = true
		}
	}
	if !found {
		t.Errorf("the swap isn't in the history: %+v", entries)
	}
}
