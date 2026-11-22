package mekbay

import (
	"testing"
)

// A seed has one MekBay account, derived from it, so no player ever holds, pastes or loses a MekBay user ID.
func TestOneAccountPerSeed(t *testing.T) {
	const seed = "ironfalcon417"
	id := AccountUID(seed, SlotHangar)
	if !UUIDPattern.MatchString(id) || id != MekBayUID(seed, SlotHangar) || id == AccountUID("wolfhound512", SlotHangar) {
		t.Errorf("the seed's account is a stable MekBay user ID of its own: %q", id)
	}
	if AccountUID("", SlotHangar) != "" {
		t.Error("no seed gives no account")
	}
}
