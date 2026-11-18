package site

import (
	"testing"
)

const (
	uidP1 = "11111111-2222-4333-8444-555555555551"
)

func TestMekBayUID(t *testing.T) {
	a := MekBayUID("ironfalcon417", SlotHangar)
	if !uuidPattern.MatchString(a) {
		t.Fatalf("not shaped like a MekBay user ID: %q", a)
	}
	if a != MekBayUID("ironfalcon417", SlotHangar) {
		t.Error("the same seed and slot must always give the same ID")
	}
	if MekBayUID("wolfhound512", SlotHangar) == a {
		t.Error("different seeds must give different IDs")
	}
	if a == playthroughID("ironfalcon417") {
		t.Error("the account ID must not be the playthrough's database key")
	}
	if accountUID("", SlotHangar) != "" || accountUID("ironfalcon417", SlotHangar) != a {
		t.Error("accountUID: no seed gives no ID, a seed gives its slot's ID")
	}
	// Hangars copied before the slot was renamed keep their owner: the derivation never changed (it was Player 1's slot, salt "p1").
	if MekBayUID("ironfalcon417", "p1") != a {
		t.Error("the hangar account must keep deriving from the historical salt")
	}
}

// A seed has one MekBay account, derived from it, so no player ever holds, pastes or loses a MekBay user ID.
func TestOneAccountPerSeed(t *testing.T) {
	const seed = "ironfalcon417"
	id := accountUID(seed, SlotHangar)
	if !uuidPattern.MatchString(id) || id != MekBayUID(seed, SlotHangar) || id == accountUID("wolfhound512", SlotHangar) {
		t.Errorf("the seed's account is a stable MekBay user ID of its own: %q", id)
	}
	if accountUID("", SlotHangar) != "" {
		t.Error("no seed gives no account")
	}
}

// A database from when players' own MekBay user IDs were kept loses them: nothing needs them any more.
func TestOldPlayerIDsAreForgotten(t *testing.T) {
	path := t.TempDir() + "/old.db"
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE player_accounts (playthrough_id TEXT NOT NULL, slot TEXT NOT NULL, uid TEXT NOT NULL, PRIMARY KEY (playthrough_id, slot))`,
		`INSERT INTO player_accounts (playthrough_id, slot, uid) VALUES ('x', 'p1', '` + uidP1 + `')`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s.db.Close()
	s, err = OpenSQLite(path) // opening runs the schema, which drops the table
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'player_accounts'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("the table of players' own IDs is gone: %d %v", n, err)
	}
}
