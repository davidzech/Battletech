package site

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

// MekBay accounts. MekBay identifiers are implicit: there is no registration, and any UUID a client presents is an account, which owns whatever forces it saves
// (only its owner may change a force). So the site does not use the player's own MekBay account at all. It synthesizes one from the seed: a hash of the seed, in the
// shape of a MekBay user ID, is the stable MekBay identity for that seed, a 1:1 projection of seed to site-managed ID. The site keeps and manages the company's
// hangar under it (see CloneHangar), and the player never sees the MekBay side: they only give a MekBay hangar link, which the site copies.
//
// A seed has up to five identities: Player 1 to 4 and the OpFor. Player 1's owns the company's hangar. The others are for the forces each player and the
// automated OpFor will hold in MekBay when deploying (not built yet).
//
// Nothing about these IDs is stored: each is recomputed from the seed (which arrives in the request's cookie) whenever it is needed. They are secrets in the sense
// that anyone holding one could change that account's forces in MekBay, so they are never rendered into a page, sent to the browser or logged; the hash is salted
// with a name unique to this purpose, so the playthrough's database key (also a hash of the seed) cannot be used to work one out.

// Account slots.
const (
	SlotPlayer1 = "p1"
	SlotPlayer2 = "p2"
	SlotPlayer3 = "p3"
	SlotPlayer4 = "p4"
	SlotOpFor   = "opfor"
)

// maxPlayers is how many players a company can have.
const maxPlayers = 4

// playerSlot is the account slot of the nth player (0-based: 0 is Player 1).
func playerSlot(n int) string { return "p" + strconv.Itoa(n+1) }

// MekBayUID is the MekBay user ID the site uses for a seed's slot: SHA-256 of a purpose-specific prefix, the slot and the seed, laid out as a version 4 UUID (which
// is what MekBay's own clients use for user IDs). The same seed and slot always give the same ID, and different slots or seeds give different ones.
func MekBayUID(seed, slot string) string {
	sum := sha256.Sum256([]byte("battletech-mekbay-account-v1:" + slot + ":" + seed))
	b := sum[:16]
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// accountUID is the MekBay user ID for the visitor's seed in a slot; "" if there is no seed.
func accountUID(seed, slot string) string {
	if seed == "" {
		return ""
	}
	return MekBayUID(seed, slot)
}

// SiteAccountUID is the MekBay user ID the site uses for the visitor's own hangar copy (Player 1's account), for the Manage page's advanced-users note. The visitor
// already holds the seed it is derived from, so showing it to them reveals nothing new; it is never shown to anyone else or put anywhere else.
func (p Page) SiteAccountUID() string { return accountUID(p.Seed, SlotPlayer1) }
