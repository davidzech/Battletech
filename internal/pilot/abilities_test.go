package pilot

import (
	"encoding/json"
	"strings"
	"testing"
)

// The twelve abilities of Hot Spots: Draconis Reach, printed p. 166.
func TestEdgeAbilityCatalog(t *testing.T) {
	want := []string{"Assassin", "Forward Observer", "Marksman", "Jumping Jack", "Patient", "Cautious", "Protector", "Coolant Flush", "Bulwark", "Melee Specialist", "Nimble", "Speed Demon"}
	if len(AbilityCatalog) != len(want) {
		t.Fatalf("%d abilities, want %d", len(AbilityCatalog), len(want))
	}
	ids := map[string]bool{}
	for i, a := range AbilityCatalog {
		if a.Name != want[i] {
			t.Errorf("ability %d is %q, want %q", i, a.Name, want[i])
		}
		if a.ID == "" || ids[a.ID] || a.Category == "" || len(a.Text) < 30 {
			t.Errorf("bad ability %+v", a)
		}
		ids[a.ID] = true
		if got, ok := AbilityByID(a.ID); !ok || got != a {
			t.Errorf("edgeAbilityByID(%q)", a.ID)
		}
	}
	if _, ok := AbilityByID("nope"); ok {
		t.Error("unknown ability found")
	}
	cats := map[string]int{}
	for _, a := range AbilityCatalog {
		cats[a.Category]++
	}
	if cats["Attack"] != 4 || cats["Defense"] != 2 || cats["Melee"] != 2 || cats["Movement"] != 2 || cats["Heat"] != 1 || cats["Indirect Fire"] != 1 {
		t.Errorf("categories: %v", cats)
	}
	// Spot-check the book's wording, and that the catalog travels to the page as JSON.
	if a, _ := AbilityByID("marksman"); !strings.Contains(a.Text, "inflict a critical hit") || !strings.Contains(a.Text, "(TW: Add 1 to your roll") {
		t.Errorf("Marksman: %q", a.Text)
	}
	var back []EdgeAbility
	if err := json.Unmarshal([]byte(AbilitiesJSON), &back); err != nil || len(back) != 12 {
		t.Errorf("catalog JSON: %v %d", err, len(back))
	}
	if got := edgeAbilityList([]string{"nimble", "gone", "marksman"}); len(got) != 2 || got[0].ID != "nimble" || got[1].ID != "marksman" {
		t.Errorf("edgeAbilityList keeps the order and skips unknown IDs: %+v", got)
	}
}
