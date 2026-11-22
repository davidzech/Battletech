// Package mekbay talks to MekBay (mekbay.com) over its unofficial WebSocket protocol: reading hangars and battles, and every change the site
// makes to a force it owns (cloning and building hangars, crews, repairs, sales, swaps, purchases, copies, battles and their write-back). It
// also works out a unit's condition the way MekBay does, parses hangar links, and derives the site's MekBay account from a seed.
package mekbay
