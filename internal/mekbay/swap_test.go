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
	if err := swapUnitState(src, "u1", "BMMadCat_A", time.Now()); err != nil {
		t.Fatal(err)
	}
	um := src["groups"].([]any)[0].(map[string]any)["units"].([]any)[0].(map[string]any)
	st := um["state"].(map[string]any)
	if um["unit"] != "BMMadCat_A" || len(st["locations"].(map[string]any)) != 0 || len(st["crits"].([]any)) != 0 || st["destroyed"] != false {
		t.Errorf("the unit should be the new variant, fresh: %+v", um)
	}
	// Swapping to the variant the unit already is (the same swap asked for twice) changes nothing, which is an error, so it isn't paid for twice.
	if err := swapUnitState(src, "u1", "BMMadCat_A", time.Now()); !errors.Is(err, ErrNoChange) {
		t.Errorf("the same swap a second time: %v", err)
	}
	if swapUnitState(src, "nope", "x", time.Now()) == nil {
		t.Error("a unit that isn't in the force can't be swapped")
	}
}
