package site

import (
	"encoding/json"
	"testing"
)

// The four states set up in the live test hangar, with the raw values MekBay stored for them (2026-11-16), run through the real parser.
const liveHangarReply = `{"data":{"name":"t","groups":[{"name":"","units":[
{"id":"a","unit":"BMVulture_Prime","state":{"destroyed":false,"locations":{"HD":{"armor":1,"internal":0}},"crits":[],"crew":[]}},
{"id":"b","unit":"BMDasher_Prime","state":{"destroyed":false,"locations":{"CT":{"armor":5,"internal":1}},"crits":[],"crew":[]}},
{"id":"c","unit":"BMBaboon","state":{"destroyed":false,
 "locations":{"LL":{"armor":0,"internal":4},"LT":{"armor":0,"internal":5},"RA":{"armor":5,"internal":3}},
 "crits":[{"name":"Clan Ammo LRM-5","loc":"RT","consumed":5,"hits":0},{"name":"Hip","loc":"LL","destroyed":1,"hits":0}],"crew":[]}},
{"id":"d","unit":"BMFenris_Prime","state":{"destroyed":true,"locations":{"CT":{"armor":19,"internal":14}},"crits":[],"crew":[]}}]}]}}`

func TestConditionsOfTheLiveHangar(t *testing.T) {
	h, err := parseHangarForce(json.RawMessage(liveHangarReply))
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		cond Condition
		ammo bool
	}{{CondArmor, false}, {CondStructure, false}, {CondCrippled, true}, {CondTruly, false}}
	views := hangarViews(h)
	for i, w := range want {
		if views[i].Condition != string(w.cond) || views[i].AmmoExpended != w.ammo {
			t.Errorf("%s: got %s ammo=%v (%s), want %s ammo=%v", h.Units[i].Unit, views[i].Condition, views[i].AmmoExpended, views[i].ConditionNote, w.cond, w.ammo)
		}
	}
}

func TestConditionPrecedenceAndCrippleRules(t *testing.T) {
	loc := func(m map[string]LocationLoss) HangarUnit { return HangarUnit{Locations: m} }
	cases := []struct {
		name   string
		hu     HangarUnit
		engine string
		want   Condition
	}{
		{"clean", HangarUnit{}, "Fusion", CondOK},
		{"destroyed beats everything", HangarUnit{Destroyed: true, EngineHits: 3}, "Fusion", CondDestroyed},
		{"center torso gone is truly destroyed", HangarUnit{Destroyed: true, Locations: map[string]LocationLoss{"CT": {Internal: 6}}}, "Fusion", CondTruly},
		{"a destroyed head alone is only destroyed", HangarUnit{Destroyed: true, Locations: map[string]LocationLoss{"HD": {Internal: 3}}}, "Fusion", CondDestroyed},
		{"two engine hits", HangarUnit{EngineHits: 2}, "XL (Clan)", CondCrippled},
		{"one engine hit is only structure-level", HangarUnit{EngineHits: 1, Locations: map[string]LocationLoss{"CT": {Internal: 1}}}, "Fusion", CondStructure},
		{"two arms alone are not crippled", loc(map[string]LocationLoss{"LA": {Internal: 3}, "RA": {Internal: 3}}), "Fusion", CondStructure},
		{"arm and leg", loc(map[string]LocationLoss{"RA": {Internal: 3}, "LL": {Internal: 4}}), "Fusion", CondCrippled},
		{"one torso, fusion, check pending", loc(map[string]LocationLoss{"LT": {Internal: 5}}), "Fusion", CondStructure},
		{"one torso, fusion, check failed", HangarUnit{Locations: map[string]LocationLoss{"LT": {Internal: 5}}, TorsoCheckFailed: true}, "Fusion", CondCrippled},
		{"one torso, XL needs no check", loc(map[string]LocationLoss{"LT": {Internal: 5}}), "XL (Clan)", CondCrippled},
		{"two torsos", loc(map[string]LocationLoss{"LT": {Internal: 5}, "RT": {Internal: 5}}), "Fusion", CondCrippled},
		{"armor only", loc(map[string]LocationLoss{"CT": {Armor: 4}}), "Fusion", CondArmor},
	}
	for _, c := range cases {
		if got, why := conditionOf(c.hu, 20, c.engine); got != c.want {
			t.Errorf("%s: got %s (%s), want %s", c.name, got, why, c.want)
		}
	}
}
