package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// MekBay accounts. MekBay only lets a force's owner change it, so every player who will hold a force in MekBay needs their own MekBay user ID, and
// the automated OpFor needs one too (it owns the OpFor force). A company can hold up to four player IDs (Player 1 to 4) and one for the OpFor.
//
// Player 1's ID is the playthrough's `owner_uid` (it owns the company's hangar, and it is what turns modify mode on, as before); the others are rows
// of `mekbay_accounts`. All of them are secrets, like a password for that account's MekBay forces: stored server-side only, never rendered back to the
// page (the pages are told only which slots are saved, never the IDs), and never logged.

// Account slots.
const (
	SlotPlayer1 = "p1"
	SlotPlayer2 = "p2"
	SlotPlayer3 = "p3"
	SlotPlayer4 = "p4"
	SlotOpFor   = "opfor"
)

// maxPlayers is how many players a company can have MekBay accounts for.
const maxPlayers = 4

// AccountSlot describes one slot for the Company details dialog: its key, its label, the names of its form fields, and whether an ID is saved.
type AccountSlot struct {
	Key        string // p1..p4 or opfor
	Label      string // "Player 1", ..., "OpFor"
	Field      string // the form field that sets it ("owner" for Player 1, for compatibility, else "owner_p2" ...)
	ClearField string // the form field that forgets it
	Saved      bool
}

var accountSlotDefs = []AccountSlot{
	{Key: SlotPlayer1, Label: "Player 1", Field: "owner", ClearField: "clear_owner"},
	{Key: SlotPlayer2, Label: "Player 2", Field: "owner_p2", ClearField: "clear_owner_p2"},
	{Key: SlotPlayer3, Label: "Player 3", Field: "owner_p3", ClearField: "clear_owner_p3"},
	{Key: SlotPlayer4, Label: "Player 4", Field: "owner_p4", ClearField: "clear_owner_p4"},
	{Key: SlotOpFor, Label: "OpFor", Field: "owner_opfor", ClearField: "clear_owner_opfor"},
}

// Slots lists every account slot with whether it has an ID saved, for the Company details dialog.
func (p Playthrough) Slots() []AccountSlot {
	out := make([]AccountSlot, len(accountSlotDefs))
	for i, d := range accountSlotDefs {
		d.Saved = p.HasAccount(d.Key)
		out[i] = d
	}
	return out
}

// HasAccount reports whether a slot has a MekBay user ID saved.
func (p Playthrough) HasAccount(slot string) bool {
	if slot == SlotPlayer1 {
		return p.OwnerUID != ""
	}
	return p.saved[slot]
}

// PlayerAccounts is whether each of Player 1 to 4 has an ID saved, in order.
func (p Playthrough) PlayerAccounts() [maxPlayers]bool {
	return [maxPlayers]bool{p.HasAccount(SlotPlayer1), p.HasAccount(SlotPlayer2), p.HasAccount(SlotPlayer3), p.HasAccount(SlotPlayer4)}
}

// loadSaved fills in which extra slots have an ID (never the IDs).
func (s *Store) loadSaved(ctx context.Context, p *Playthrough) error {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT slot FROM mekbay_accounts WHERE playthrough_id = ?`), p.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	p.saved = map[string]bool{}
	for rows.Next() {
		var slot string
		if err := rows.Scan(&slot); err != nil {
			return err
		}
		p.saved[slot] = true
	}
	return rows.Err()
}

// SetAccount saves a slot's MekBay user ID; an empty uid forgets it.
func (s *Store) SetAccount(ctx context.Context, id, slot, uid string) error {
	switch slot {
	case SlotPlayer1:
		return s.SetOwnerUID(ctx, id, uid)
	case SlotPlayer2, SlotPlayer3, SlotPlayer4, SlotOpFor:
	default:
		return errors.New("unknown account slot")
	}
	if uid == "" {
		_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM mekbay_accounts WHERE playthrough_id = ? AND slot = ?`), id, slot)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		s.q(`INSERT INTO mekbay_accounts (playthrough_id, slot, uid) VALUES (?, ?, ?) ON CONFLICT (playthrough_id, slot) DO UPDATE SET uid = excluded.uid`), id, slot, uid)
	return err
}

// AccountUID is a slot's MekBay user ID, for the server's own use (it is never sent to a page); ok is false if the slot has none.
func (s *Store) AccountUID(ctx context.Context, id, slot string) (uid string, ok bool, err error) {
	if slot == SlotPlayer1 {
		pt, found, err := s.Playthrough(ctx, id)
		return pt.OwnerUID, found && pt.OwnerUID != "", err
	}
	err = s.db.QueryRowContext(ctx, s.q(`SELECT uid FROM mekbay_accounts WHERE playthrough_id = ? AND slot = ?`), id, slot).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return uid, err == nil, err
}

// AccountsJSON tells the Deploy wizard which MekBay accounts are saved (never the IDs): whether each of Player 1 to 4 has one, and the OpFor.
func (p Page) AccountsJSON() string {
	b, err := json.Marshal(map[string]any{"players": p.Play.PlayerAccounts(), "opfor": p.Play.HasAccount(SlotOpFor)})
	if err != nil {
		return "{}"
	}
	return string(b)
}
