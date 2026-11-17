package site

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

const (
	uidP2    = "11111111-2222-3333-4444-555555555552"
	uidP3    = "11111111-2222-3333-4444-555555555553"
	uidOpFor = "11111111-2222-3333-4444-555555555559"
)

// accountStore is part of the store contract (see storeContract), so it runs against SQLite and, in CI, Postgres.
func accountStore(t *testing.T, s *Store, id string) {
	t.Helper()
	ctx := context.Background()
	pt, _, err := s.Playthrough(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if pt.HasAccount(SlotPlayer2) || pt.HasAccount(SlotOpFor) || len(pt.Slots()) != 5 {
		t.Fatalf("a fresh playthrough has no extra accounts: %+v", pt.Slots())
	}
	if err := s.SetAccount(ctx, id, SlotPlayer2, uidP2); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccount(ctx, id, SlotOpFor, uidOpFor); err != nil {
		t.Fatal(err)
	}
	// Saving again replaces the ID rather than adding a row.
	if err := s.SetAccount(ctx, id, SlotPlayer2, uidP3); err != nil {
		t.Fatal(err)
	}
	pt, _, _ = s.Playthrough(ctx, id)
	if !pt.HasAccount(SlotPlayer2) || !pt.HasAccount(SlotOpFor) || pt.HasAccount(SlotPlayer3) || pt.HasAccount(SlotPlayer4) {
		t.Errorf("saved slots: %+v", pt.Slots())
	}
	if uid, ok, err := s.AccountUID(ctx, id, SlotPlayer2); err != nil || !ok || uid != uidP3 {
		t.Errorf("player 2's ID: %q %v %v", uid, ok, err)
	}
	if _, ok, _ := s.AccountUID(ctx, id, SlotPlayer4); ok {
		t.Error("player 4 has no ID")
	}
	// Player 1's ID is the owner UID, and what turns modify mode on.
	if err := s.SetAccount(ctx, id, SlotPlayer1, uidP2); err != nil {
		t.Fatal(err)
	}
	pt, _, _ = s.Playthrough(ctx, id)
	if !pt.HasAccount(SlotPlayer1) || !pt.CanModify() || pt.PlayerAccounts() != [maxPlayers]bool{true, true, false, false} {
		t.Errorf("player 1: %+v", pt.PlayerAccounts())
	}
	// An empty ID forgets the slot.
	for _, slot := range []string{SlotPlayer1, SlotPlayer2, SlotOpFor} {
		if err := s.SetAccount(ctx, id, slot, ""); err != nil {
			t.Fatal(err)
		}
	}
	pt, _, _ = s.Playthrough(ctx, id)
	if pt.HasAccount(SlotPlayer1) || pt.HasAccount(SlotPlayer2) || pt.HasAccount(SlotOpFor) || pt.CanModify() {
		t.Errorf("forgotten slots: %+v", pt.Slots())
	}
	if err := s.SetAccount(ctx, id, "p9", uidP2); err == nil {
		t.Error("an unknown slot should be refused")
	}
}

func TestCompanyAccounts(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	id := playthroughID("wolfpack2026")
	save := func(extra url.Values) int {
		v := url.Values{"company": {"Wolf Pack"}}
		for k, vs := range extra {
			v[k] = vs
		}
		return post(t, srv, "/api/company", v, cookie, srv.URL).StatusCode
	}
	if got := save(url.Values{"owner": {ownerUID}, "owner_p2": {uidP2}, "owner_p3": {uidP3}, "owner_opfor": {uidOpFor}}); got != http.StatusNoContent {
		t.Fatalf("save: %d", got)
	}
	pt, _, _ := store.Playthrough(ctx, id)
	if pt.PlayerAccounts() != [maxPlayers]bool{true, true, true, false} || !pt.HasAccount(SlotOpFor) || !pt.CanModify() {
		t.Fatalf("saved: %+v", pt.Slots())
	}
	// A bad ID is refused and says which slot, before anything is saved.
	before := pt.PlayerAccounts()
	r := post(t, srv, "/api/company", url.Values{"company": {"Renamed"}, "owner_p4": {uidP2}, "owner_p2": {"not-a-uuid"}}, cookie, srv.URL)
	if r.StatusCode != http.StatusBadRequest {
		t.Errorf("bad ID: %d", r.StatusCode)
	}
	if pt2, _, _ := store.Playthrough(ctx, id); pt2.CompanyName != "Wolf Pack" || pt2.PlayerAccounts() != before {
		t.Errorf("a refused save must change nothing: %q %+v", pt2.CompanyName, pt2.PlayerAccounts())
	}
	// Blank fields keep what is saved; a clear field forgets one slot only.
	if got := save(nil); got != http.StatusNoContent {
		t.Fatalf("blank save: %d", got)
	}
	if pt, _, _ = store.Playthrough(ctx, id); pt.PlayerAccounts() != before {
		t.Errorf("blank fields keep the saved IDs: %+v", pt.PlayerAccounts())
	}
	if got := save(url.Values{"clear_owner_p3": {"1"}}); got != http.StatusNoContent {
		t.Fatalf("clear: %d", got)
	}
	pt, _, _ = store.Playthrough(ctx, id)
	if pt.HasAccount(SlotPlayer3) || !pt.HasAccount(SlotPlayer2) || !pt.HasAccount(SlotOpFor) || !pt.CanModify() {
		t.Errorf("clearing Player 3 only: %+v", pt.Slots())
	}
	// The pages say which slots are saved, but never an ID.
	page := body(t, srv, "/refusalwar/", cookie)
	for _, secret := range []string{ownerUID, uidP2, uidP3, uidOpFor} {
		if strings.Contains(page, secret) {
			t.Errorf("a MekBay user ID was rendered into the page")
		}
	}
	for _, want := range []string{`name="owner"`, `name="owner_p2"`, `name="owner_p3"`, `name="owner_p4"`, `name="owner_opfor"`, `name="clear_owner_p2"`, `name="clear_owner_opfor"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the company dialog is missing %s", want)
		}
	}
}
