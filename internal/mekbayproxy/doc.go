// Package mekbayproxy is the MekBay proxy: MekBay's own web app, served from host names of ours, each one acting as the MekBay account its link names.
//
// Why: MekBay keeps a visitor's identity (a UUID) in the browser's IndexedDB, and an account owns its forces. A force owned by an account
// nobody holds (the site's OpFor, a player's derived account) would be read-only in a stock browser. The proxy lets the site hand a player
// a plain link that opens MekBay already acting as the right account, with nothing to paste and nothing to lose.
//
// How: two parts, both in this program (MekBay's server is closed source and its front end is a stack we don't want to host or fork):
//
//   - The web app. MekBay's prebuilt static files are fetched from mekbay.com on demand, cached and served unchanged except for two things:
//     the one JavaScript string that names its WebSocket (wss://mekbay.com/ws), which now names our relay on the same host, and the service
//     workers (ngsw-worker.js, safety-worker.js), which become stubs that unregister themselves, because a rewritten file would fail the
//     worker's own hash checks. Everything else is untouched, so the app is always MekBay's current build and stays in step with its server.
//   - The relay (/ws). It accepts the browser's WebSocket, opens one to mekbay.com and passes the messages through, replacing the "uuid" of
//     every message the browser sends with the account of the link the browser was reached through. The browser still believes it is a random
//     visitor.
//
// The identity is the host name (package storage, links.go): https://mekbay-<id>.<site host>/. Every link is its own origin, so two links open in two tabs
// of one browser are two accounts with separate cookies, IndexedDB and tabs, and a link keeps working when its tab is closed and reopened. The
// site's own cookies never reach these hosts, and MekBay's JavaScript is cross-origin to the site's APIs. Every response carries
// Referrer-Policy: no-referrer, so following a link out of MekBay never leaks the host name.
//
// A link host that isn't valid gets no pages and no relay. The one exception to "no link, no relay" is a developer's machine: reached as
// mekbay.localhost from the same machine (sandboxAllowed), the relay acts as one shared sandbox account, so MekBay works when opened directly.
//
// This relies on MekBay's unofficial, unpublished protocol and on the WebSocket string surviving its build; Check says whether it does.
package mekbayproxy
