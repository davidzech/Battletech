package mekbay

import (
	"testing"
)

func TestRemoveUnitFromForce(t *testing.T) {
	force := map[string]any{"groups": []any{
		map[string]any{"name": "A", "units": []any{map[string]any{"id": "u1"}, map[string]any{"id": "u2"}}},
		map[string]any{"name": "B", "units": []any{map[string]any{"id": "u3"}}},
	}}
	if !removeUnitFromForce(force, "u2") || removeUnitFromForce(force, "u2") || removeUnitFromForce(force, "nope") {
		t.Fatal("remove should succeed once and fail for a missing unit")
	}
	a := force["groups"].([]any)[0].(map[string]any)["units"].([]any)
	if len(a) != 1 || a[0].(map[string]any)["id"] != "u1" || len(force["groups"].([]any)[1].(map[string]any)["units"].([]any)) != 1 {
		t.Errorf("only u2 should be gone: %+v", force)
	}
}
