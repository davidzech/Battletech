package site

// MekBay accounts. MekBay identifiers are implicit: there is no registration, and any UUID a client presents is an account, which owns what it saves (only
// its owner may change a force). A seed has exactly one MekBay account, **synthesized** from the seed so nobody has to hold, paste or keep a MekBay user ID:
// MekBayUID is a salted SHA-256 of the seed laid out as a v4 UUID (what MekBay's own clients use for user IDs), a stable 1:1 projection of seed to site-managed
// ID, recomputed from the seed whenever needed and never shown to anyone but the seed's holder. It is a secret, like a password for that account's forces:
// whoever has it can change them. Everything the site keeps in MekBay for the seed is owned by it: the campaign's hangar copy, the OpFor, each player's force
// in a battle, and the battle's operation, so everyone at the table can edit all of it. A browser acts as the account through a MekBay link (mekbaylinks.go),
// which seals it into a host name of its own, so a player opens MekBay from a link and never sees an ID.

// notOwnedMessage is what the hangar-changing endpoints say about a run that predates the site's own copy of the hangar.
const notOwnedMessage = "Your hangar is from before the site kept its own copy. Import it again (Manage force, Danger zone, Re-import hangar) first."
