package mekbay

import (
	"testing"
)

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
		if got, why := ConditionOf(c.hu, 20, c.engine); got != c.want {
			t.Errorf("%s: got %s (%s), want %s", c.name, got, why, c.want)
		}
	}
}
