package mekbay

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCloneForceIsPristine(t *testing.T) {
	var src map[string]any
	if err := json.Unmarshal([]byte(wornForce), &src); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	got, err := CleanForce(src, "new-instance", now)
	if err != nil {
		t.Fatal(err)
	}
	if got["instanceId"] != "new-instance" || got["owned"] != nil {
		t.Errorf("the copy has its own instance and nothing the server fills in: %v %v", got["instanceId"], got["owned"])
	}
	if src["instanceId"] != "src" {
		t.Error("cloning must not change the source")
	}
	groups := got["groups"].([]any)
	if len(groups) != 1 || groups[0].(map[string]any)["name"] != "Deployed" {
		t.Fatalf("the arrangement is kept: %+v", groups)
	}
	for _, u := range groups[0].(map[string]any)["units"].([]any) {
		um := u.(map[string]any)
		st := um["state"].(map[string]any)
		if len(st["locations"].(map[string]any)) != 0 || st["destroyed"] != false || st["ruleChecks"] != nil {
			t.Errorf("%v: damage should be gone: %+v", um["id"], st)
		}
		if h := st["heat"].(map[string]any); h["current"] != 0 || h["previous"] != 0 {
			t.Errorf("heat: %v", h)
		}
		crew := st["crew"].([]any)
		c := crew[0].(map[string]any)
		if len(crew) != 1 || c["name"] != "" || c["gunnerySkill"] != DefaultGunnery || c["pilotingSkill"] != DefaultPiloting || c["hits"] != 0 {
			t.Errorf("%v: crew should be one unnamed 4/5 pilot, unwounded: %+v", um["id"], crew)
		}
		crits, _ := st["crits"].([]any)
		for _, cr := range crits {
			cm := cr.(map[string]any)
			if cm["destroyed"] == true || cm["consumed"] != nil && cm["consumed"] != 0 {
				t.Errorf("crit not cleared: %+v", cm)
			}
		}
	}
	// What a reader makes of the copy: nothing damaged, nobody named.
	b, _ := json.Marshal(map[string]any{"data": got})
	h, err := ParseHangarForce(b)
	if err != nil || len(h.Units) != 2 {
		t.Fatalf("parse the copy: %v %+v", err, h)
	}
	for _, u := range h.Units {
		if u.Damaged || u.Destroyed || u.Pilot != "" || u.AmmoBins != 0 {
			t.Errorf("the copy should read as new: %+v", u)
		}
	}
}
