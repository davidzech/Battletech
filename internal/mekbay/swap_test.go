package mekbay

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestSwapUnitState(t *testing.T) {
	var src map[string]any
	if err := json.Unmarshal([]byte(wornForce), &src); err != nil {
		t.Fatal(err)
	}
	if err := SwapUnitState(src, "u1", "BMMadCat_A", time.Now()); err != nil {
		t.Fatal(err)
	}
	um := src["groups"].([]any)[0].(map[string]any)["units"].([]any)[0].(map[string]any)
	st := um["state"].(map[string]any)
	if um["unit"] != "BMMadCat_A" || len(st["locations"].(map[string]any)) != 0 || len(st["crits"].([]any)) != 0 || st["destroyed"] != false {
		t.Errorf("the unit should be the new variant, fresh: %+v", um)
	}
	// Swapping to the variant the unit already is (the same swap asked for twice) changes nothing, which is an error, so it isn't paid for twice.
	if err := SwapUnitState(src, "u1", "BMMadCat_A", time.Now()); !errors.Is(err, ErrNoChange) {
		t.Errorf("the same swap a second time: %v", err)
	}
	if SwapUnitState(src, "nope", "x", time.Now()) == nil {
		t.Error("a unit that isn't in the force can't be swapped")
	}
}

// The new variant carries nothing of the old one's state: no inventory (its equipment is different), no conditions, no turn, no rule checks, no crits.
func TestSwapDropsLeftovers(t *testing.T) {
	var f map[string]any
	if err := json.Unmarshal([]byte(`{"name":"t","groups":[{"name":"","units":[{"id":"u","unit":"BMMadCat_H","state":{"destroyed":false,
		"conditions":["prone"],"turnState":{"turnCounter":2},"ruleChecks":{"core.torso-crippling":{"trigger":"LT","status":"failed"}},
		"inventory":[{"id":"CLLRM20@LT#0","destroyed":true,"consumed":3,"states":[{"name":"mode","value":"x"}]}],
		"crits":[{"name":"Hip","loc":"LL","pendingHits":1,"hitTimestamps":[1]}],"crew":[]}}]}]}`), &f); err != nil {
		t.Fatal(err)
	}
	if err := SwapUnitState(f, "u", "BMMadCat_A", time.Now()); err != nil {
		t.Fatal(err)
	}
	st := stateOf(f, "u")
	for _, k := range []string{"conditions", "turnState", "ruleChecks", "inventory"} {
		if st[k] != nil {
			t.Errorf("%s is left over from the old variant: %v", k, st[k])
		}
	}
	if len(st["crits"].([]any)) != 0 {
		t.Errorf("crits: %v", st["crits"])
	}
}
