package site

import (
	"errors"
	"fmt"
)

// The multiplayer lobby operation. A Deploy will end in a MekBay operation the players join as a lobby: one force for each player, owned by that player's own
// MekBay account so they can run it, and the OpFor's force(s), owned by the site. This file is the backend's plan for it, with no MekBay calls yet: given a
// validated deployment plan and the accounts, it says which account owns each force and what goes in it, so the MekBay writer that comes next only has to
// carry the plan out. It is built (and so checked) by the dry-run POST /api/deploy/plan.
//
// Ownership:
//
//	each player's force   the player's own MekBay user ID (Player 1 to 4, saved in the company settings)
//	the OpFor's forces    the synthesized OpFor account (MekBayUID, SlotOpFor)
//	the hangar            the synthesized hangar account (MekBayUID, SlotHangar): the units stay there, and a player's force is built from the units they took
//
// The IDs are secrets. LobbyForce.Owner never leaves the server: it is excluded from JSON and must not be logged.

// LobbyForce is one force of the lobby operation.
type LobbyForce struct {
	Name   string `json:"name"`   // "Player 1", "OpFor"
	Player int    `json:"player"` // 1 to 4 for a player's force, 0 for the OpFor's
	Owner  string `json:"-"`      // the MekBay account that owns the force (secret)
	// Units are the hangar units a player takes (IDs inside the hangar force); the OpFor's units come from the scenario's .mul files instead.
	Units []string         `json:"units,omitempty"`
	Crew  []CrewAssignment `json:"-"` // the named pilots the player assigned, applied to the units of their force
}

// LobbyPlan is everything a Deploy will create in MekBay.
type LobbyPlan struct {
	Hangar  string       `json:"-"` // owner of the hangar the units are taken from (secret)
	Players []LobbyForce `json:"players"`
	OpFor   LobbyForce   `json:"opfor"`
}

// buildLobbyPlan assigns the accounts for a plan: units[i] and crews[i] are player i+1's mechs and pilot assignments, playerUIDs the saved MekBay user IDs. It
// refuses a plan whose players lack an ID, and IDs that collide with each other or with the site's own accounts (a player must never own the OpFor).
func buildLobbyPlan(seed string, playerUIDs [maxPlayers]string, units [][]string, crews [][]CrewAssignment) (LobbyPlan, error) {
	n := len(units)
	if n < 1 || n > maxPlayers {
		return LobbyPlan{}, fmt.Errorf("a lobby has 1 to %d players", maxPlayers)
	}
	var saved [maxPlayers]bool
	for i, uid := range playerUIDs {
		saved[i] = uid != ""
	}
	if msg := playerIDsProblem(saved, n); msg != "" {
		return LobbyPlan{}, errors.New(msg)
	}
	plan := LobbyPlan{Hangar: accountUID(seed, SlotHangar), OpFor: LobbyForce{Name: "OpFor", Owner: accountUID(seed, SlotOpFor)}}
	if plan.Hangar == "" || plan.OpFor.Owner == "" {
		return LobbyPlan{}, errors.New("a lobby needs a seed")
	}
	owners := map[string]string{plan.Hangar: "the hangar", plan.OpFor.Owner: "the OpFor"}
	for i := 0; i < n; i++ {
		uid := playerUIDs[i]
		if who, dup := owners[uid]; dup {
			return LobbyPlan{}, fmt.Errorf("%s's MekBay user ID is the same as %s's, and every force needs its own owner.", playerLabel(i), who)
		}
		owners[uid] = playerLabel(i)
		f := LobbyForce{Name: playerLabel(i), Player: i + 1, Owner: uid, Units: units[i]}
		if i < len(crews) {
			f.Crew = crews[i]
		}
		plan.Players = append(plan.Players, f)
	}
	return plan, nil
}

// lobbyForceView is how a plan describes a force to the page: who it belongs to, never the account.
type lobbyForceView struct {
	Name  string `json:"name"`
	Owner string `json:"owner"` // "player" (their own MekBay account) or "site" (managed by the site)
	Units int    `json:"units"` // mechs taken from the hangar (0 for the OpFor, whose units come from the scenario)
}

// View lists the lobby's forces for the plan response.
func (p LobbyPlan) View() []lobbyForceView {
	out := make([]lobbyForceView, 0, len(p.Players)+1)
	for _, f := range p.Players {
		out = append(out, lobbyForceView{Name: f.Name, Owner: "player", Units: len(f.Units)})
	}
	return append(out, lobbyForceView{Name: p.OpFor.Name, Owner: "site"})
}
