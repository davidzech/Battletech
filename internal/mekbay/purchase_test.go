package mekbay

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAddUnitsToForce(t *testing.T) {
	var force map[string]any
	if err := json.Unmarshal([]byte(crippledForce), &force); err != nil {
		t.Fatal(err)
	}
	// There is no Hangar group yet (the one group has no name): a new one is made, and the old unit is untouched.
	addUnitsToForce(force, []string{"BMMadCat_H", "BMKoshi_E"}, time.Now())
	b, _ := json.Marshal(map[string]any{"data": force})
	h, err := ParseHangarForce(b)
	if err != nil || len(h.Units) != 3 {
		t.Fatalf("parse: %v, %d units", err, len(h.Units))
	}
	if h.Units[0].Unit != "BMBaboon" || h.Units[1].Unit != "BMMadCat_H" || h.Units[2].Unit != "BMKoshi_E" || h.Units[1].Group != "Hangar" {
		t.Errorf("units: %+v", h.Units)
	}
	if c, _ := ConditionOf(h.Units[1], 60, "XL (Clan)"); c != CondOK || h.Units[1].Gunnery != 4 || h.Units[1].Piloting != 5 || h.Units[1].ID == "" || h.Units[1].ID == h.Units[2].ID {
		t.Errorf("a bought unit should be fresh, 4/5, with its own ID: %+v", h.Units[1])
	}
	// A second purchase joins the same Hangar group.
	addUnitsToForce(force, []string{"BMGrizzly"}, time.Now())
	groups, _ := force["groups"].([]any)
	if len(groups) != 2 {
		t.Errorf("expected the original group and one Hangar group, got %d", len(groups))
	}
}
