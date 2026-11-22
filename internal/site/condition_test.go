package site

import (
	"encoding/json"
	"testing"

	"github.com/davidzech/Battletech/internal/mekbay"
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
	h, err := mekbay.ParseHangarForce(json.RawMessage(liveHangarReply))
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		cond mekbay.Condition
		ammo bool
	}{{mekbay.CondArmor, false}, {mekbay.CondStructure, false}, {mekbay.CondCrippled, true}, {mekbay.CondTruly, false}}
	views := hangarViews(h)
	for i, w := range want {
		if views[i].Condition != string(w.cond) || views[i].AmmoExpended != w.ammo {
			t.Errorf("%s: got %s ammo=%v (%s), want %s ammo=%v", h.Units[i].Unit, views[i].Condition, views[i].AmmoExpended, views[i].ConditionNote, w.cond, w.ammo)
		}
	}
}
