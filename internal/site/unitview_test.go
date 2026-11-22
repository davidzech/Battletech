package site

import (
	"encoding/json"
	"strings"
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

func TestHangarViewsAndDeployProblem(t *testing.T) {
	h := hangarFixture()
	views := hangarViews(h)
	if v := views[0]; v.Name != "Mad Dog" || v.Model != "Prime" || v.Icon != "/icons/vulture.png" || v.BV != 2351 || v.Pilot != "Anya" || v.Class != "Heavy" || v.Tons != 60 || v.InDeployed {
		t.Errorf("vulture view = %+v", v)
	}
	if v := views[1]; !v.InDeployed || !v.Damaged || v.BV <= 0 {
		t.Errorf("loki view = %+v", v)
	}
	if v := views[7]; !v.Unknown || v.Name != "BMNoSuchMech_X" {
		t.Errorf("unknown view = %+v", v)
	}
	for _, c := range []struct {
		ids   []string
		limit int
		want  string // substring of the problem, or "" for fine
	}{
		{[]string{"u-vulture", "u-loki1"}, 10000, ""},
		{[]string{"u-vulture", "u-loki1"}, 0, ""},
		{nil, 10000, "at least one"},
		{[]string{"u-vulture", "u-vulture"}, 0, "twice"},
		{[]string{"u-ghost"}, 0, "no longer in your hangar"},
		{[]string{"u-dead"}, 0, "destroyed"},
		{[]string{"u-odd"}, 0, "unit catalog"},
		{[]string{"u-loki1", "u-loki2", "u-loki3", "u-loki4", "u-loki5"}, 10000, "over this scenario's limit of 10000"},
		{[]string{"u-loki1", "u-loki2", "u-loki3", "u-loki4", "u-loki5"}, 0, ""},
	} {
		got := deployProblem(h, c.ids, c.limit)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("deployProblem(%v, %d) = %q, want it to contain %q", c.ids, c.limit, got, c.want)
		}
	}
}
