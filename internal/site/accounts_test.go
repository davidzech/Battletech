package site

import (
	"testing"

	"github.com/davidzech/Battletech/internal/mekbay"
)

func TestMekBayUID(t *testing.T) {
	a := mekbay.MekBayUID("ironfalcon417", mekbay.SlotHangar)
	if !mekbay.UUIDPattern.MatchString(a) {
		t.Fatalf("not shaped like a MekBay user ID: %q", a)
	}
	if a != mekbay.MekBayUID("ironfalcon417", mekbay.SlotHangar) {
		t.Error("the same seed and slot must always give the same ID")
	}
	if mekbay.MekBayUID("wolfhound512", mekbay.SlotHangar) == a {
		t.Error("different seeds must give different IDs")
	}
	if a == playthroughID("ironfalcon417") {
		t.Error("the account ID must not be the playthrough's database key")
	}
	if mekbay.AccountUID("", mekbay.SlotHangar) != "" || mekbay.AccountUID("ironfalcon417", mekbay.SlotHangar) != a {
		t.Error("accountUID: no seed gives no ID, a seed gives its slot's ID")
	}
	// Hangars copied before the slot was renamed keep their owner: the derivation never changed (it was Player 1's slot, salt "p1").
	if mekbay.MekBayUID("ironfalcon417", "p1") != a {
		t.Error("the hangar account must keep deriving from the historical salt")
	}
}
