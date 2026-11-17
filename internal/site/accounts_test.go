package site

import (
	"context"
	"testing"
)

const (
	uidP1 = "11111111-2222-4333-8444-555555555551"
	uidP2 = "11111111-2222-4333-8444-555555555552"
	uidP3 = "11111111-2222-4333-8444-555555555553"
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

func TestPlayerAccounts(t *testing.T) {
	s, err := OpenSQLite(t.TempDir() + "/accounts.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Close() })
	ctx := context.Background()
	const id = "playthrough-a"
	if _, err := s.EnsurePlaythrough(ctx, id); err != nil {
		t.Fatal(err)
	}
	if pt, _, _ := s.Playthrough(ctx, id); pt.PlayerIDs != [maxPlayers]bool{} {
		t.Fatalf("a new company has no player IDs: %+v", pt.PlayerIDs)
	}
	if err := s.SetPlayerAccount(ctx, id, 0, uidP1); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlayerAccount(ctx, id, 2, uidP3); err != nil {
		t.Fatal(err)
	}
	pt, _, _ := s.Playthrough(ctx, id)
	if pt.PlayerIDs != [maxPlayers]bool{true, false, true, false} {
		t.Errorf("saved flags: %+v", pt.PlayerIDs)
	}
	uids, err := s.PlayerUIDs(ctx, id)
	if err != nil || uids != [maxPlayers]string{uidP1, "", uidP3, ""} {
		t.Errorf("uids: %+v %v", uids, err)
	}
	// Saving again replaces, an empty ID forgets, and another company is untouched.
	_ = s.SetPlayerAccount(ctx, id, 0, uidP2)
	_ = s.SetPlayerAccount(ctx, id, 2, "")
	uids, _ = s.PlayerUIDs(ctx, id)
	if uids != [maxPlayers]string{uidP2, "", "", ""} {
		t.Errorf("after replace and forget: %+v", uids)
	}
	if _, err := s.EnsurePlaythrough(ctx, "playthrough-b"); err != nil {
		t.Fatal(err)
	}
	if other, _ := s.PlayerUIDs(ctx, "playthrough-b"); other != [maxPlayers]string{} {
		t.Errorf("another company has its own IDs: %+v", other)
	}
	if err := s.SetPlayerAccount(ctx, id, maxPlayers, uidP1); err == nil {
		t.Error("there is no player 5")
	}
	// The pages are told which are saved, never the IDs.
	p := Page{Play: pt}
	if p.PlayerIDsJSON() != "[true,false,true,false]" {
		t.Errorf("PlayerIDsJSON = %s", p.PlayerIDsJSON())
	}
}

func TestPlayerIDChecks(t *testing.T) {
	if got, err := parsePlayerUID("  " + uidP1 + " "); err != nil || got != uidP1 {
		t.Errorf("parsePlayerUID = %q, %v", got, err)
	}
	if got, err := parsePlayerUID(""); err != nil || got != "" {
		t.Errorf("blank is allowed: %q %v", got, err)
	}
	for _, bad := range []string{"nope", uidP1 + "x", "https://mekbay.com/?instance=" + goodInstance} {
		if _, err := parsePlayerUID(bad); err == nil {
			t.Errorf("parsePlayerUID(%q) should fail", bad)
		}
	}
	saved := [maxPlayers]bool{true, false, true, false}
	if playerIDsProblem(saved, 1) != "" {
		t.Error("one player needs only Player 1")
	}
	if got := playerIDsProblem(saved, 2); got != "Player 2 needs a MekBay user ID: add it under Account details." {
		t.Errorf("two players: %q", got)
	}
	if got := playerIDsProblem([maxPlayers]bool{}, 2); got != "Player 1 and Player 2 need MekBay user IDs: add them under Account details." {
		t.Errorf("none saved: %q", got)
	}
	slots := Playthrough{PlayerIDs: saved}.Slots()
	if len(slots) != 4 || slots[0].Field != "uid_p1" || slots[1].ClearField != "clear_uid_p2" || !slots[0].Saved || slots[1].Saved || slots[2].Label != "Player 3" {
		t.Errorf("slots: %+v", slots)
	}
}
