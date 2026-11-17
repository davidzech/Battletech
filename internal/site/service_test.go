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

func TestRepairCostsFollowTheSPActivityCostTable(t *testing.T) {
	cases := []struct {
		level string
		tons  float64
		tech  string
		want  int
	}{
		{RepairArmor, 20, "Inner Sphere", 10}, {RepairStructure, 20, "Inner Sphere", 40},
		{RepairCrippled, 20, "Inner Sphere", 60}, {RepairDestroyed, 20, "Inner Sphere", 100},
		{RepairArmor, 20, "Clan", 15}, {RepairStructure, 20, "Clan", 60}, {RepairCrippled, 20, "Clan", 90}, {RepairDestroyed, 20, "Clan", 150},
		{RepairArmor, 25, "Mixed", 19},        // 12.5 x 1.5 = 18.75, rounded up
		{RepairArmor, 25, "Inner Sphere", 13}, // 12.5 rounded up
		{RepairDestroyed, 100, "Clan", 750},
	}
	for _, c := range cases {
		if got := repairCost(c.level, c.tons, c.tech); got != c.want {
			t.Errorf("%s %vt %s: %d, want %d", c.level, c.tons, c.tech, got, c.want)
		}
	}
	if rearmCost(3) != 30 {
		t.Errorf("rearm of three bins: %d", rearmCost(3))
	}
}

func TestRepairOptions(t *testing.T) {
	loc := func(m map[string]LocationLoss) HangarUnit { return HangarUnit{Locations: m} }
	cases := []struct {
		name string
		hu   HangarUnit
		cond Condition
		want string
	}{
		{"undamaged", HangarUnit{}, CondOK, ""},
		{"armor only", loc(map[string]LocationLoss{"CT": {Armor: 4}}), CondArmor, "armor"},
		{"structure only, no armor lost", loc(map[string]LocationLoss{"CT": {Internal: 2}}), CondStructure, "structure"},
		{"armor and structure", loc(map[string]LocationLoss{"CT": {Armor: 4, Internal: 2}}), CondStructure, "armor,structure"},
		{"a crit in a living location is structure-level", HangarUnit{Crits: []CritDamage{{Loc: "RT"}}}, CondStructure, "structure"},
		{"crippled by destroyed limbs, armor also lost", loc(map[string]LocationLoss{"LL": {Internal: 4}, "RA": {Armor: 5, Internal: 3}}), CondCrippled, "armor,crippled"},
		{"crippled with surviving structure damage too", loc(map[string]LocationLoss{"LL": {Internal: 4}, "RA": {Internal: 3}, "CT": {Internal: 1}}), CondCrippled, "structure,crippled"},
		{"crippled by engine hits only", HangarUnit{EngineHits: 2, Crits: []CritDamage{{Loc: "CT", Engine: true}}}, CondCrippled, "crippled"},
		{"destroyed", HangarUnit{Destroyed: true, Locations: map[string]LocationLoss{"HD": {Armor: 1, Internal: 3}}}, CondDestroyed, "armor,crippled,destroyed"},
		{"truly destroyed", loc(map[string]LocationLoss{"CT": {Internal: 6}}), CondTruly, ""},
	}
	for _, c := range cases {
		if got := strings.Join(repairOptions(c.hu, c.cond, 20), ","); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// The Howler's raw state from the live hangar (2026-11-16): crippled, with one spent ammo bin.
const crippledForce = `{"name":"t","groups":[{"name":"","units":[{"id":"c","unit":"BMBaboon","updatedTs":1,"state":{"destroyed":false,
 "locations":{"LL":{"armor":0,"internal":4},"LT":{"armor":0,"internal":5},"RA":{"armor":5,"internal":3}},
 "ruleChecks":{"core.torso-crippling":{"status":"failed"}},
 "crits":[{"name":"Clan Ammo LRM-5","loc":"RT","consumed":5,"hits":0},{"name":"Hip","loc":"LL","destroyed":1,"destroying":1,"destroyedTurn":0,"hits":0}],"crew":[]}}]}]}`

func readBack(t *testing.T, force map[string]any) HangarUnit {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"data": force})
	h, err := parseHangarForce(b)
	if err != nil || len(h.Units) != 1 {
		t.Fatalf("parse: %v %+v", err, h)
	}
	return h.Units[0]
}

func TestServiceUnitState(t *testing.T) {
	load := func() map[string]any {
		var f map[string]any
		if err := json.Unmarshal([]byte(crippledForce), &f); err != nil {
			t.Fatal(err)
		}
		return f
	}
	repaired := func(level string) HangarUnit {
		f := load()
		if err := serviceUnitState(f, "c", "repair", level, 20); err != nil {
			t.Fatal(err)
		}
		return readBack(t, f)
	}
	before := readBack(t, load())
	if c, _ := conditionOf(before, 20, "Fusion"); c != CondCrippled || before.AmmoBins != 1 || before.Locations["RA"].Armor != 5 {
		t.Fatalf("before: %s, %d bins", c, before.AmmoBins)
	}
	// Armor only: the armor goes, and the unit is just as crippled (its structure is untouched).
	a := repaired(RepairArmor)
	if c, _ := conditionOf(a, 20, "Fusion"); c != CondCrippled || a.Locations["RA"].Armor != 0 || a.Locations["RA"].Internal != 3 || len(a.Crits) != 1 {
		t.Errorf("armor repair: %s %+v crits %d", c, a.Locations, len(a.Crits))
	}
	// Structure: nothing is left to fix in a surviving location, so the destroyed limbs, their crits and the cripple all stay.
	st := repaired(RepairStructure)
	if c, _ := conditionOf(st, 20, "Fusion"); c != CondCrippled || st.Locations["LL"].Internal != 4 || len(st.Crits) != 1 || st.Locations["RA"].Armor != 0 {
		t.Errorf("structure repair: %s %+v crits %d", c, st.Locations, len(st.Crits))
	}
	// Crippled: everything is repaired, but spent ammunition waits for a rearm.
	cr := repaired(RepairCrippled)
	if c, _ := conditionOf(cr, 20, "Fusion"); c != CondOK || cr.Destroyed || cr.AmmoBins != 1 || len(cr.Crits) != 0 {
		t.Errorf("crippled repair: %s destroyed=%v bins=%d crits=%d", c, cr.Destroyed, cr.AmmoBins, len(cr.Crits))
	}
	// A rearm refills the ammunition and leaves the damage.
	f := load()
	if err := serviceUnitState(f, "c", "rearm", "", 20); err != nil {
		t.Fatal(err)
	}
	ra := readBack(t, f)
	if c, _ := conditionOf(ra, 20, "Fusion"); c != CondCrippled || ra.AmmoBins != 0 || ra.AmmoUsed != 0 {
		t.Errorf("after rearm: %s bins=%d used=%d", c, ra.AmmoBins, ra.AmmoUsed)
	}
	if err := serviceUnitState(load(), "nope", "repair", RepairArmor, 20); err == nil {
		t.Error("an unknown unit should be refused")
	}
	if err := serviceUnitState(load(), "c", "paint", "", 20); err == nil {
		t.Error("an unknown service should be refused")
	}
	if err := serviceUnitState(load(), "c", "repair", "polish", 20); err == nil {
		t.Error("an unknown repair level should be refused")
	}

	// A unit with structure damage in a surviving location, a critical hit there and an engine hit: structure repair fixes the first two only.
	var g map[string]any
	_ = json.Unmarshal([]byte(`{"groups":[{"units":[{"id":"x","unit":"BMBaboon","state":{"destroyed":false,
	 "locations":{"CT":{"armor":3,"internal":2}},
	 "crits":[{"name":"Gyro","loc":"CT","destroyed":1,"hits":0},{"name":"Clan XL Engine","loc":"CT","destroyed":1,"hits":0}],"crew":[]}}]}]}`), &g)
	if err := serviceUnitState(g, "x", "repair", RepairStructure, 20); err != nil {
		t.Fatal(err)
	}
	x := readBack(t, g)
	if len(x.Locations) != 0 || len(x.Crits) != 1 || !x.Crits[0].Engine {
		t.Errorf("structure repair should leave only the engine hit: %+v %+v", x.Locations, x.Crits)
	}
}

func TestServiceUnitEndpoint(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	post(t, srv, "/api/company", url.Values{"company": {"Wolf Pack"}}, cookie, srv.URL) // read-only: no owner UID
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	id := playthroughID("wolfpack2026")
	// Dasher (20t Clan) is crippled with spent ammo; Koshi is truly destroyed; the Grizzly is undamaged.
	h := hangarOf("BMDasher_Prime", "BMKoshi_E", "BMGrizzly", "BMUller_J")
	h.Units[0].EngineHits, h.Units[0].AmmoBins, h.Units[0].AmmoUsed = 2, 2, 8
	h.Units[1].Destroyed, h.Units[1].Locations = true, map[string]LocationLoss{"CT": {Internal: 8}}
	h.Units[3].Locations = map[string]LocationLoss{"LA": {Armor: 3, Internal: 1}}
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
	if got := status(form("kind", "rearm", "unit", "ua")); got != http.StatusForbidden || funds() != 4500 {
		t.Errorf("read-only (no MekBay user ID): %d, funds %d", got, funds())
	}
	// Add the owner UID: modify mode.
	if r := post(t, srv, "/api/company", url.Values{"company": {"Wolf Pack"}, "owner": {ownerUID}}, cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("owner: %d", r.StatusCode)
	}

	// Rearm: 10 SP per spent bin, two bins.
	if got := status(form("kind", "rearm", "unit", "ua")); got != http.StatusOK || funds() != 4480 {
		t.Fatalf("rearm: %d, funds %d", got, funds())
	}
	if len(fake.services) != 1 || fake.services[0].Kind != "rearm" || fake.services[0].UnitID != "ua" || fake.services[0].OwnerUID != ownerUID {
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
	h.Units[0].Locations = map[string]LocationLoss{"HD": {Armor: 1}}
	h.Units[0].AmmoBins, h.Units[0].AmmoUsed = 1, 3
	h.Units[1].Destroyed, h.Units[1].Locations = true, map[string]LocationLoss{"CT": {Internal: 8}}
	v := hangarViews(h)
	if strings.Join(v[0].RepairLevels, ",") != "armor" || v[0].RepairCosts[RepairCrippled] != 90 || v[0].RearmCost != 10 {
		t.Errorf("damaged Dasher: %+v", v[0])
	}
	if v[1].Condition != string(CondTruly) || len(v[1].RepairLevels) != 0 {
		t.Errorf("truly destroyed unit: %+v", v[1])
	}
	if b, _ := json.Marshal(v[1]); !strings.Contains(string(b), `"repairLevels":null`) {
		t.Errorf("the view should say there is no repair level: %s", b)
	}
}

func TestAmmoBreakdown(t *testing.T) {
	baboon, ok := unitByName("BMBaboon")
	if !ok || len(baboon.Ammo) == 0 {
		t.Fatalf("the unit index should carry the Howler's ammunition: %+v", baboon)
	}
	// The raw state of the live Howler: one LRM 5 bin (of three, 24 shots a ton) with 5 shots used.
	var f map[string]any
	if err := json.Unmarshal([]byte(crippledForce), &f); err != nil {
		t.Fatal(err)
	}
	hu := readBack(t, f)
	got := ammoBreakdown(hu, baboon)
	if len(got) != 1 || got[0] != (ammoLine{Name: "LRM 5 Ammo", Tons: 1, Used: 5, Capacity: 24}) {
		t.Errorf("Howler: %+v", got)
	}
	// Two bins of one type add up, and different types are listed apart, in name order; a bin the catalog doesn't know is still a ton.
	grizzly, _ := unitByName("BMGrizzly")
	hu = HangarUnit{AmmoBins: 4, AmmoUsed2: []AmmoUse{
		{Name: "Clan Ammo LRM-10", Loc: "LT", Used: 4}, {Name: "Clan Gauss Ammo", Loc: "RA", Used: 3}, {Name: "Clan Gauss Ammo", Loc: "RA", Used: 5}, {Name: "Mystery Ammo", Loc: "CT", Used: 2},
	}}
	got = ammoBreakdown(hu, grizzly)
	want := []ammoLine{{"Gauss Ammo", 2, 8, 16}, {"LRM 10 Ammo", 1, 4, 12}, {"Mystery Ammo", 1, 2, 0}}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("Grizzly: %+v, want %+v", got, want)
	}
	// The view carries it, and the cost is the tons times 10.
	h := hangarOf("BMBaboon")
	h.Units[0] = HangarUnit{ID: "ua", Unit: "BMBaboon", AmmoBins: 1, AmmoUsed: 5, AmmoUsed2: []AmmoUse{{Name: "Clan Ammo LRM-5", Loc: "RT", Used: 5}}}
	v := hangarViews(h)[0]
	if v.RearmCost != 10 || len(v.AmmoBreakdown) != 1 || v.AmmoBreakdown[0].Tons != 1 {
		t.Errorf("view: %+v", v)
	}
}
