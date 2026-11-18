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
	if MekBayUID("ironfalcon417", SlotOpFor) == a {
		t.Error("the hangar and the OpFor are different accounts")
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

// Every account the site uses is derived from the seed, so no player ever holds, pastes or loses a MekBay user ID.
func TestPlayerAccountsAreDerived(t *testing.T) {
	const seed = "ironfalcon417"
	if SlotPlayer(1) != "player1" || SlotPlayer(4) != "player4" {
		t.Errorf("slots: %q %q", SlotPlayer(1), SlotPlayer(4))
	}
	seen := map[string]string{MekBayUID(seed, SlotHangar): "the hangar", MekBayUID(seed, SlotOpFor): "the OpFor"}
	for n := 1; n <= maxPlayers; n++ {
		id := MekBayUID(seed, SlotPlayer(n))
		if !uuidPattern.MatchString(id) || id != accountUID(seed, SlotPlayer(n)) || id != MekBayUID(seed, SlotPlayer(n)) {
			t.Errorf("player %d's account is a stable MekBay user ID: %q", n, id)
		}
		if who, dup := seen[id]; dup {
			t.Errorf("player %d's account is the same as %s's", n, who)
		}
		seen[id] = playerLabel(n - 1)
		if MekBayUID("wolfhound512", SlotPlayer(n)) == id {
			t.Errorf("player %d: another seed must give another account", n)
		}
	}
	if accountUID("", SlotPlayer(1)) != "" {
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
