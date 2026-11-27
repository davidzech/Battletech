package site

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/hangar"
	"github.com/davidzech/Battletech/internal/mekbay"
)

// The Howler's raw state from the live hangar (2026-11-16): crippled, with one spent ammo bin.
const crippledForce = `{"name":"t","groups":[{"name":"","units":[{"id":"c","unit":"BMBaboon","updatedTs":1,"state":{"destroyed":false,
 "locations":{"LL":{"armor":0,"internal":4},"LT":{"armor":0,"internal":5},"RA":{"armor":5,"internal":3}},
 "ruleChecks":{"core.torso-crippling":{"status":"failed"}},
 "crits":[{"name":"Clan Ammo LRM-5","loc":"RT","consumed":5,"hits":0},{"name":"Hip","loc":"LL","destroyed":1,"destroying":1,"destroyedTurn":0,"hits":0}],"crew":[]}}]}]}`

func readBack(t *testing.T, force map[string]any) mekbay.HangarUnit {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"data": force})
	h, err := mekbay.ParseHangarForce(b)
	if err != nil || len(h.Units) != 1 {
		t.Fatalf("parse: %v %+v", err, h)
	}
	return h.Units[0]
}

func TestServiceUnitEndpoint(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	// These tests are about prices, not the campaign's starting funds: give them a round balance.
	if _, err := store.DB().Exec(`UPDATE campaign_runs SET warchest = 4500`); err != nil {
		t.Fatal(err)
	}
	id := playthroughID("wolfpack2026")
	// Dasher (20t Clan) is crippled with spent ammo; Koshi is truly destroyed; the Grizzly is undamaged.
	h := hangarOf("BMDasher_Prime", "BMKoshi_E", "BMGrizzly", "BMUller_J")
	h.Units[0].EngineHits, h.Units[0].AmmoBins, h.Units[0].AmmoUsed = 2, 2, 8
	h.Units[1].Destroyed, h.Units[1].Locations = true, map[string]mekbay.LocationLoss{"CT": {Internal: 8}}
	h.Units[3].Locations = map[string]mekbay.LocationLoss{"LA": {Armor: 3, Internal: 1}}
	fake.setHangar(h, nil)
	form := func(kv ...string) url.Values {
		v := url.Values{"campaign": {"refusalwar"}}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Set(kv[i], kv[i+1])
		}
		return v
	}
	status := func(v url.Values) int { return post(t, srv, "/api/hangar/service", v, cookie, srv.URL).StatusCode }
	funds := func() int { r, _, _ := store.Run(ctx, id, "refusalwar"); return r.Warchest }
	if funds() != 4500 {
		t.Fatalf("start funds %d", funds())
	}

	if got := post(t, srv, "/api/hangar/service", form("kind", "rearm", "unit", "ua"), cookie, "https://evil.example").StatusCode; got != http.StatusForbidden {
		t.Errorf("cross-origin: %d", got)
	}
	// A run from before the site kept its own copy of the hangar can't be changed.
	_, _ = store.DB().Exec(`UPDATE campaign_runs SET source_url = ''`)
	if got := status(form("kind", "rearm", "unit", "ua")); got != http.StatusConflict || funds() != 4500 {
		t.Errorf("a hangar the site doesn't own: %d, funds %d", got, funds())
	}
	_, _ = store.DB().Exec(`UPDATE campaign_runs SET source_url = ?`, goodHangarURL)

	// Rearm: 10 SP per spent bin, two bins.
	if got := status(form("kind", "rearm", "unit", "ua")); got != http.StatusOK || funds() != 4480 {
		t.Fatalf("rearm: %d, funds %d", got, funds())
	}
	if len(fake.services) != 1 || fake.services[0].Kind != "rearm" || fake.services[0].UnitID != "ua" || fake.services[0].OwnerUID != mekbay.MekBayUID("wolfpack2026", mekbay.SlotHangar) {
		t.Errorf("MekBay was asked: %+v", fake.services)
	}
	// Repair: the level must match the unit's condition (crippled, 20t Clan: 90 SP).
	if got := status(form("kind", "repair", "unit", "ua", "level", "armor")); got != http.StatusConflict || funds() != 4480 {
		t.Errorf("wrong level: %d, funds %d", got, funds())
	}
	if got := status(form("kind", "repair", "unit", "ua", "level", "crippled")); got != http.StatusOK || funds() != 4390 {
		t.Errorf("repair: %d, funds %d", got, funds())
	}
	// Truly destroyed, undamaged and unknown units are refused, and nothing is charged.
	for name, v := range map[string]url.Values{
		"truly destroyed": form("kind", "repair", "unit", "ub", "level", "destroyed"),
		"undamaged":       form("kind", "repair", "unit", "uc", "level", "armor"),
		"nothing spent":   form("kind", "rearm", "unit", "uc"),
		"unknown unit":    form("kind", "repair", "unit", "zz", "level", "armor"),
		"bad kind":        form("kind", "paint", "unit", "ua"),
	} {
		if got := status(v); got < 400 || funds() != 4390 {
			t.Errorf("%s: %d, funds %d", name, got, funds())
		}
	}
	// Armor and structure are separate choices: the Kit Fox (30t Clan) has lost both, and can have the armor alone repaired (30 x 0.5 x 1.5 = 23 SP).
	if got := status(form("kind", "repair", "unit", "ud", "level", "crippled")); got != http.StatusConflict || funds() != 4390 {
		t.Errorf("a level that does not apply: %d, funds %d", got, funds())
	}
	if got := status(form("kind", "repair", "unit", "ud", "level", "armor")); got != http.StatusOK || funds() != 4367 {
		t.Errorf("armor only: %d, funds %d", got, funds())
	}
	if last := fake.services[len(fake.services)-1]; last.Level != "armor" || last.Kind != "repair" || last.UnitID != "ud" || last.Tons != 30 {
		t.Errorf("MekBay was asked: %+v", last)
	}
	// One destroyed arm does not cripple the Kit Fox, so it is structure damage (30 x 2 x 1.5 = 90 SP): the crippled level isn't offered, and the structure
	// repair is told the unit isn't crippled, which is what lets it restore the arm.
	h.Units[3].Locations = map[string]mekbay.LocationLoss{"LA": {Internal: 5}}
	fake.setHangar(h, nil)
	if got := status(form("kind", "repair", "unit", "ud", "level", "crippled")); got != http.StatusConflict || funds() != 4367 {
		t.Errorf("crippled-level repair of a unit that isn't crippled: %d, funds %d", got, funds())
	}
	if got := status(form("kind", "repair", "unit", "ud", "level", "structure")); got != http.StatusOK || funds() != 4277 {
		t.Errorf("structure repair of a destroyed arm: %d, funds %d", got, funds())
	}
	if last := fake.services[len(fake.services)-1]; last.Level != mekbay.RepairStructure || last.Crippled {
		t.Errorf("the structure repair sent to MekBay: %+v", last)
	}
	if hist, _ := store.History(ctx, id, "refusalwar"); len(hist) == 0 || hist[len(hist)-1].Text != "Repaired Kit Fox J (structure and critical hits) for 90 SP." {
		t.Errorf("history: %+v", hist)
	}
	// Not enough funds, and a MekBay failure refunds the charge.
	if err := store.SetWarchest(ctx, id, "refusalwar", 50); err != nil {
		t.Fatal(err)
	}
	if got := status(form("kind", "repair", "unit", "ua", "level", "crippled")); got != http.StatusConflict || funds() != 50 {
		t.Errorf("too poor: %d, funds %d", got, funds())
	}
	fake.mu.Lock()
	fake.serviceErr = errors.New("MekBay said no")
	fake.mu.Unlock()
	if got := status(form("kind", "rearm", "unit", "ua")); got != http.StatusBadGateway || funds() != 50 {
		t.Errorf("MekBay failure: %d, funds %d (should be refunded)", got, funds())
	}
}

func TestServiceCostsInTheHangarViews(t *testing.T) {
	h := hangarOf("BMDasher_Prime", "BMKoshi_E")
	h.Units[0].Locations = map[string]mekbay.LocationLoss{"HD": {Armor: 1}}
	h.Units[0].AmmoBins, h.Units[0].AmmoUsed = 1, 3
	h.Units[1].Destroyed, h.Units[1].Locations = true, map[string]mekbay.LocationLoss{"CT": {Internal: 8}}
	v := hangarViews(h)
	if strings.Join(v[0].RepairLevels, ",") != "armor" || v[0].RepairCosts[mekbay.RepairCrippled] != 90 || v[0].RearmCost != 10 {
		t.Errorf("damaged Dasher: %+v", v[0])
	}
	if v[1].Condition != string(mekbay.CondTruly) || len(v[1].RepairLevels) != 0 {
		t.Errorf("truly destroyed unit: %+v", v[1])
	}
	if b, _ := json.Marshal(v[1]); !strings.Contains(string(b), `"repairLevels":null`) {
		t.Errorf("the view should say there is no repair level: %s", b)
	}
	// Damage that doesn't cripple still has a repair: a lone critical hit, a lone engine hit, one destroyed arm (a 20-ton arm has 3 points).
	h = hangarOf("BMDasher_Prime", "BMDasher_Prime", "BMDasher_Prime")
	h.Units[0].Damaged, h.Units[0].Crits = true, []mekbay.CritDamage{{Loc: "RT"}}
	h.Units[1].Damaged, h.Units[1].EngineHits, h.Units[1].Crits = true, 1, []mekbay.CritDamage{{Loc: "CT", Engine: true}}
	h.Units[2].Damaged, h.Units[2].Locations = true, map[string]mekbay.LocationLoss{"LA": {Internal: 3}}
	v = hangarViews(h)
	for i, want := range []string{"structure", "structure", "structure"} {
		if v[i].Condition != string(mekbay.CondStructure) || strings.Join(v[i].RepairLevels, ",") != want || v[i].SellFull {
			t.Errorf("unit %d: condition %s (%s), repairs %v, want %s", i, v[i].Condition, v[i].ConditionNote, v[i].RepairLevels, want)
		}
	}
}

func TestAmmoBreakdown(t *testing.T) {
	baboon, ok := catalog.ByName("BMBaboon")
	if !ok || len(baboon.Ammo) == 0 {
		t.Fatalf("the unit index should carry the Howler's ammunition: %+v", baboon)
	}
	// The raw state of the live Howler: one LRM 5 bin (of three, 24 shots a ton) with 5 shots used.
	var f map[string]any
	if err := json.Unmarshal([]byte(crippledForce), &f); err != nil {
		t.Fatal(err)
	}
	hu := readBack(t, f)
	got := hangar.AmmoBreakdown(hu, baboon)
	if len(got) != 1 || got[0] != (hangar.AmmoLine{Name: "LRM 5 Ammo", Tons: 1, Used: 5, Capacity: 24}) {
		t.Errorf("Howler: %+v", got)
	}
	// Two bins of one type add up, and different types are listed apart, in name order; a bin the catalog doesn't know is still a ton.
	grizzly, _ := catalog.ByName("BMGrizzly")
	hu = mekbay.HangarUnit{AmmoBins: 4, AmmoUsed2: []mekbay.AmmoUse{
		{Name: "Clan Ammo LRM-10", Loc: "LT", Used: 4}, {Name: "Clan Gauss Ammo", Loc: "RA", Used: 3}, {Name: "Clan Gauss Ammo", Loc: "RA", Used: 5}, {Name: "Mystery Ammo", Loc: "CT", Used: 2},
	}}
	got = hangar.AmmoBreakdown(hu, grizzly)
	want := []hangar.AmmoLine{{Name: "Gauss Ammo", Tons: 2, Used: 8, Capacity: 16}, {Name: "LRM 10 Ammo", Tons: 1, Used: 4, Capacity: 12}, {Name: "Mystery Ammo", Tons: 1, Used: 2, Capacity: 0}}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("Grizzly: %+v, want %+v", got, want)
	}
	// The view carries it, and the cost is the tons times 10.
	h := hangarOf("BMBaboon")
	h.Units[0] = mekbay.HangarUnit{ID: "ua", Unit: "BMBaboon", AmmoBins: 1, AmmoUsed: 5, AmmoUsed2: []mekbay.AmmoUse{{Name: "Clan Ammo LRM-5", Loc: "RT", Used: 5}}}
	v := hangarViews(h)[0]
	if v.RearmCost != 10 || len(v.AmmoBreakdown) != 1 || v.AmmoBreakdown[0].Tons != 1 {
		t.Errorf("view: %+v", v)
	}
}
