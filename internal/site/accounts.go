package site

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// MekBay accounts. MekBay identifiers are implicit: there is no registration, and any UUID a client presents is an account, which owns whatever forces it saves
// (only its owner may change a force). A company uses two kinds of them.
//
// Real ones. Each physical player has their own MekBay user ID (Player 1 to 4), which the company's settings keep (table player_accounts). They are what lets a
// player hold their own force in the multiplayer lobby operation a Deploy builds: the force a player takes onto the field is owned by their account. They are
// secrets, like a password for that account's MekBay forces: stored server-side only, never rendered back to a page (the pages are only told which are saved)
// and never logged.
//
// Synthesized ones. The site's own bookkeeping lives under accounts nobody holds: the hangar (the site's clean copy of the company's force, see clone.go) and the
// OpFor forces a Deploy builds are owned by IDs derived from the seed (MekBayUID, a salted SHA-256 laid out as a v4 UUID: a stable 1:1 projection of seed to
// site-managed ID, one per purpose). They are recomputed from the seed whenever needed, never stored, and never logged. The player never sees the MekBay side of
// the hangar; the Manage page shows the hangar's instance and owner for advanced users only.

// Account slots of the synthesized accounts.
const (
	// SlotHangar derives the account that owns the company's hangar copy. Its derivation salt is "p1" for historical reasons (it was once Player 1's slot), so
	// hangars already copied keep working.
	SlotHangar = "hangar"
	// SlotOpFor derives the account that owns the OpFor forces.
	SlotOpFor = "opfor"
)

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

// PlayerSlot describes one player's MekBay user ID field in the company's settings.
type PlayerSlot struct {
	N          int    // 1 to 4
	Label      string // "Player 1"
	Field      string // the form field that sets it
	ClearField string // the checkbox that forgets it
	Saved      bool
}

// Slots lists the four players' ID fields with whether each has a saved ID (never the ID itself).
func (p Playthrough) Slots() []PlayerSlot {
	out := make([]PlayerSlot, maxPlayers)
	for i := range out {
		out[i] = PlayerSlot{N: i + 1, Label: playerLabel(i), Field: fmt.Sprintf("uid_p%d", i+1), ClearField: fmt.Sprintf("clear_uid_p%d", i+1), Saved: p.PlayerIDs[i]}
	}
	return out
}

// missingPlayerIDs names the players among the first n who have no saved MekBay user ID.
func missingPlayerIDs(saved [maxPlayers]bool, n int) []string {
	var out []string
	for i := 0; i < n && i < maxPlayers; i++ {
		if !saved[i] {
			out = append(out, playerLabel(i))
		}
	}
	return out
}

// playerIDsProblem is the message for a start or deploy that needs the first n players' IDs, or "" when they are all saved.
func playerIDsProblem(saved [maxPlayers]bool, n int) string {
	miss := missingPlayerIDs(saved, n)
	switch len(miss) {
	case 0:
		return ""
	case 1:
		return miss[0] + " needs a MekBay user ID: add it under Company details."
	}
	return strings.Join(miss, " and ") + " need MekBay user IDs: add them under Company details."
}

// PlayerIDsJSON tells a page which players have a saved ID, as JSON booleans (never the IDs), for the pickers that offer only counts the IDs cover.
func (p Page) PlayerIDsJSON() string {
	var b strings.Builder
	b.WriteByte('[')
	for i, saved := range p.Play.PlayerIDs {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%t", saved)
	}
	b.WriteByte(']')
	return b.String()
}

func playerSlotKey(i int) string { return fmt.Sprintf("p%d", i+1) }

// loadPlayerIDs fills in which players have a saved ID.
func (s *Store) loadPlayerIDs(ctx context.Context, p *Playthrough) error {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT slot FROM player_accounts WHERE playthrough_id = ?`), p.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var slot string
		if err := rows.Scan(&slot); err != nil {
			return err
		}
		for i := 0; i < maxPlayers; i++ {
			if slot == playerSlotKey(i) {
				p.PlayerIDs[i] = true
			}
		}
	}
	return rows.Err()
}

// SetPlayerAccount saves a player's (0-based) MekBay user ID for a playthrough; an empty uid forgets it.
func (s *Store) SetPlayerAccount(ctx context.Context, playthroughID string, player int, uid string) error {
	if player < 0 || player >= maxPlayers {
		return errors.New("no such player")
	}
	if uid == "" {
		_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM player_accounts WHERE playthrough_id = ? AND slot = ?`), playthroughID, playerSlotKey(player))
		return err
	}
	_, err := s.db.ExecContext(ctx, s.q(`INSERT INTO player_accounts (playthrough_id, slot, uid) VALUES (?, ?, ?)
		ON CONFLICT (playthrough_id, slot) DO UPDATE SET uid = excluded.uid`), playthroughID, playerSlotKey(player), uid)
	return err
}

// PlayerUIDs returns the players' saved MekBay user IDs ("" for a player with none). It is for the server's own MekBay calls: never send the result to a page or log it.
func (s *Store) PlayerUIDs(ctx context.Context, playthroughID string) (out [maxPlayers]string, err error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT slot, uid FROM player_accounts WHERE playthrough_id = ?`), playthroughID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var slot, uid string
		if err := rows.Scan(&slot, &uid); err != nil {
			return out, err
		}
		for i := 0; i < maxPlayers; i++ {
			if slot == playerSlotKey(i) {
				out[i] = uid
			}
		}
	}
	return out, rows.Err()
}

// parsePlayerUID validates a MekBay user ID the visitor typed: a UUID, in any letter case; "" is allowed and means "no change".
func parsePlayerUID(raw string) (string, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return "", nil
	}
	if !uuidPattern.MatchString(raw) {
		return "", errors.New("That doesn't look like a MekBay user ID (it should be a UUID).")
	}
	return raw, nil
}
