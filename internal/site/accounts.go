package site

import (
	"crypto/sha256"
	"encoding/hex"
)

// MekBay accounts. MekBay identifiers are implicit: there is no registration, and any UUID a client presents is an account, which owns what it saves (only
// its owner may change a force). A seed has exactly one MekBay account, **synthesized** from the seed so nobody has to hold, paste or keep a MekBay user ID:
// MekBayUID is a salted SHA-256 of the seed laid out as a v4 UUID (what MekBay's own clients use for user IDs), a stable 1:1 projection of seed to site-managed
// ID, recomputed from the seed whenever needed and never shown to anyone but the seed's holder. It is a secret, like a password for that account's forces:
// whoever has it can change them. Everything the site keeps in MekBay for the seed is owned by it: the campaign's hangar copy, the OpFor, each player's force
// in a battle, and the battle's operation, so everyone at the table can edit all of it. A browser acts as the account through a MekBay link (mekbaylinks.go),
// which seals it into a host name of its own, so a player opens MekBay from a link and never sees an ID.

// SlotHangar derives the seed's account (see above). The name and the derivation salt "p1" are historical (it was the hangar's owner when the site gave each
// purpose its own account), kept so that hangars already copied stay owned by it.
const SlotHangar = "hangar"

// maxPlayers is how many players a company can have.
const maxPlayers = 4

// MekBayUID is the MekBay user ID the site uses for a seed's synthesized account: SHA-256 of a purpose-specific prefix, the slot and the seed, laid out as a
// version 4 UUID (which is what MekBay's own clients use for user IDs). The same seed and slot always give the same ID, and different slots or seeds give different
// ones.
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

// notOwnedMessage is what the hangar-changing endpoints say about a run that predates the site's own copy of the hangar.
const notOwnedMessage = "Your hangar is from before the site kept its own copy. Import it again (Manage force, Danger zone, Re-import hangar) first."
