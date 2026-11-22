package mekbay

import (
	"encoding/json"
	"testing"
)

// A force as MekBay saves it, with every kind of wear: lost armor and structure, a destroyed flag, a critical hit, spent ammo, heat, a torso check, and a
// named, veteran, wounded crew.
const wornForce = `{"instanceId":"src","owned":true,"name":"Mine","groups":[{"id":"g1","name":"Deployed","units":[
 {"id":"u1","unit":"BMMadCat_H","state":{"locations":{"CT":{"armor":10,"internal":4}},"destroyed":true,"heat":{"current":9,"previous":3},
  "ruleChecks":{"torso":"failed"},"crits":[{"loc":"RT","slot":2,"destroyed":true},{"loc":"LA","slot":1,"consumed":6}],
  "crew":[{"id":0,"name":"Alrek","gunnerySkill":2,"pilotingSkill":3,"hits":3,"state":1}]}},
 {"id":"u2","unit":"BMKoshi_E"}]}]}`

const goodInstance = "01a12f9e-188f-7158-ad67-3fc9700ec733"

// The Howler's raw state from the live hangar (2026-11-16): crippled, with one spent ammo bin.
const crippledForce = `{"name":"t","groups":[{"name":"","units":[{"id":"c","unit":"BMBaboon","updatedTs":1,"state":{"destroyed":false,
 "locations":{"LL":{"armor":0,"internal":4},"LT":{"armor":0,"internal":5},"RA":{"armor":5,"internal":3}},
 "ruleChecks":{"core.torso-crippling":{"status":"failed"}},
 "crits":[{"name":"Clan Ammo LRM-5","loc":"RT","consumed":5,"hits":0},{"name":"Hip","loc":"LL","destroyed":1,"destroying":1,"destroyedTurn":0,"hits":0}],"crew":[]}}]}]}`

func readBack(t *testing.T, force map[string]any) HangarUnit {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"data": force})
	h, err := ParseHangarForce(b)
	if err != nil || len(h.Units) != 1 {
		t.Fatalf("parse: %v %+v", err, h)
	}
	return h.Units[0]
}
