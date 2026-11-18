package site

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// MekBay accounts. MekBay identifiers are implicit: there is no registration, and any UUID a client presents is an account, which owns what it saves (only
// its owner may change a force). The site gives every account it needs a **synthesized** one, derived from the seed so nobody has to hold, paste or keep a
// MekBay user ID: MekBayUID is a salted SHA-256 of the seed laid out as a v4 UUID (what MekBay's own clients use for user IDs), a stable 1:1 projection of seed
// to site-managed ID, one per purpose, recomputed from the seed whenever needed and never shown to anyone but the seed's holder. They are secrets, like a
// password for that account's forces: whoever has one can change them. There is one for the **hangar** (the site's clean copy of the campaign's force), one for
// the **OpFor**, and one for each **player** (Player 1 to 4), which owns that player's force in the multiplayer lobby a Deploy builds. A browser acts as one of
// them through a MekBay link (mekbaylinks.go), which seals the account into a host name of its own, so a player opens their force from a link and never sees an ID.

// Account slots of the synthesized accounts.
const (
	// SlotHangar derives the account that owns the company's hangar copy. Its derivation salt is "p1" for historical reasons (it was once Player 1's slot), so
	// hangars already copied keep working.
	SlotHangar = "hangar"
	// SlotOpFor derives the account that owns the OpFor forces.
	SlotOpFor = "opfor"
)

// SlotPlayer derives the account that owns player n's (1 to 4) force in the lobby.
func SlotPlayer(n int) string { return fmt.Sprintf("player%d", n) }

// maxPlayers is how many players a company can have.
const maxPlayers = 4

// playerLabel is the name of the nth player (0-based).
func playerLabel(n int) string { return fmt.Sprintf("Player %d", n+1) }

// MekBayUID is the MekBay user ID the site uses for a seed's synthesized account (SlotHangar or SlotOpFor): SHA-256 of a purpose-specific prefix, the slot and the
// seed, laid out as a version 4 UUID (which is what MekBay's own clients use for user IDs). The same seed and slot always give the same ID, and different slots or
// seeds give different ones.
func MekBayUID(seed, slot string) string {
	salt := slot
	if slot == SlotHangar {
		salt = "p1"
	}
	sum := sha256.Sum256([]byte("battletech-mekbay-account-v1:" + salt + ":" + seed))
	b := sum[:16]
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// accountUID is the synthesized MekBay user ID for the visitor's seed in a slot; "" if there is no seed.
func accountUID(seed, slot string) string {
	if seed == "" {
		return ""
	}
	return MekBayUID(seed, slot)
}

// SiteAccountUID is the synthesized account that owns the visitor's hangar copy, for the Manage page's advanced-users note. The visitor already holds the seed it is
// derived from, so showing it to them reveals nothing new; it is never shown to anyone else or put anywhere else.
func (p Page) SiteAccountUID() string { return accountUID(p.Seed, SlotHangar) }

// notOwnedMessage is what the hangar-changing endpoints say about a run that predates the site's own copy of the hangar.
const notOwnedMessage = "Your hangar is from before the site kept its own copy. Import it again (Manage force, Danger zone, Re-import hangar) first."
