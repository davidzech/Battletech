// Package mekbay talks to MekBay (mekbay.com) over its unofficial WebSocket protocol: building, reading and deleting battles, and reading a force by its ID. It
// also holds the edits the site makes to a hangar in MekBay's save format (repairs, rearms, sales, swaps, purchases, crews, a battle's write-back), which the site
// runs on the hangar it keeps in its database, works out a unit's condition the way MekBay does, parses hangar links, and derives the site's MekBay account from a
// seed.
package mekbay
