package site

import "testing"

func TestMekBayUID(t *testing.T) {
	a := MekBayUID("ironfalcon417", SlotPlayer1)
	if !uuidPattern.MatchString(a) {
		t.Fatalf("not shaped like a MekBay user ID: %q", a)
	}
	if a != MekBayUID("ironfalcon417", SlotPlayer1) {
		t.Error("the same seed and slot must always give the same ID")
	}
	seen := map[string]string{a: "p1"}
	for _, slot := range []string{SlotPlayer2, SlotPlayer3, SlotPlayer4, SlotOpFor} {
		id := MekBayUID("ironfalcon417", slot)
		if prev, dup := seen[id]; dup {
			t.Errorf("%s and %s share an ID", slot, prev)
		}
		seen[id] = slot
	}
	if MekBayUID("wolfhound512", SlotPlayer1) == a {
		t.Error("different seeds must give different IDs")
	}
	if a == playthroughID("ironfalcon417") {
		t.Error("the account ID must not be the playthrough's database key")
	}
	if accountUID("", SlotPlayer1) != "" || accountUID("ironfalcon417", SlotPlayer1) != a {
		t.Error("accountUID: no seed gives no ID, a seed gives its slot's ID")
	}
	if playerSlot(0) != SlotPlayer1 || playerSlot(3) != SlotPlayer4 {
		t.Error("playerSlot")
	}
}
