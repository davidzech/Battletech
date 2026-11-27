package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/davidzech/Battletech/internal/mekbay"
)

// hangarClaimStore checks ClaimHangarEdit and ReleaseHangarEdit on a playthrough that has started "refusalwar" (part of storeContract, so it runs on both
// databases).
func hangarClaimStore(t *testing.T, s *Store, id string) {
	t.Helper()
	ctx := context.Background()
	if _, ok, err := s.ClaimHangarEdit(ctx, id, "not-started"); err != nil || ok {
		t.Fatalf("a run that doesn't exist can't be claimed: ok=%v err=%v", ok, err)
	}
	first, ok, err := s.ClaimHangarEdit(ctx, id, "refusalwar")
	if err != nil || !ok || first == "" {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}
	if _, ok, _ := s.ClaimHangarEdit(ctx, id, "refusalwar"); ok {
		t.Fatal("a second claim must wait for the first")
	}
	if tok, ok, _ := s.ClaimHangarEdit(ctx, id, "another"); !ok {
		t.Fatal("another campaign's hangar is claimed separately")
	} else {
		_ = s.ReleaseHangarEdit(ctx, id, "another", tok)
	}
	if err := s.ReleaseHangarEdit(ctx, id, "refusalwar", "not-the-token"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.ClaimHangarEdit(ctx, id, "refusalwar"); ok {
		t.Fatal("releasing with the wrong token must keep the claim")
	}
	if err := s.ReleaseHangarEdit(ctx, id, "refusalwar", first); err != nil {
		t.Fatal(err)
	}
	second, ok, _ := s.ClaimHangarEdit(ctx, id, "refusalwar")
	if !ok || second == first {
		t.Fatalf("a released claim can be taken again, with a new token: ok=%v", ok)
	}

	// A claim past its TTL (its server died mid-edit) is taken over, and its first holder releasing it afterwards doesn't end the new claim.
	if _, err := s.db.ExecContext(ctx, s.q(`UPDATE campaign_runs SET hangar_claim = ? WHERE playthrough_id = ? AND campaign = ?`),
		time.Now().Add(-HangarClaimTTL-time.Minute).Unix(), id, "refusalwar"); err != nil {
		t.Fatal(err)
	}
	third, ok, _ := s.ClaimHangarEdit(ctx, id, "refusalwar")
	if !ok {
		t.Fatal("a stale claim should be taken over")
	}
	_ = s.ReleaseHangarEdit(ctx, id, "refusalwar", second)
	if _, ok, _ := s.ClaimHangarEdit(ctx, id, "refusalwar"); ok {
		t.Fatal("the stale holder's release must not end the claim that took over")
	}
	if err := s.ReleaseHangarEdit(ctx, id, "refusalwar", third); err != nil {
		t.Fatal(err)
	}
}

// A database from before hangar claims gets the claim columns when it is opened.
func TestHangarClaimColumnsAreAdded(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`ALTER TABLE campaign_runs DROP COLUMN hangar_claim`, `ALTER TABLE campaign_runs DROP COLUMN hangar_claim_token`} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.EnsurePlaythrough(ctx, "old"); err != nil {
		t.Fatal(err)
	}
	if err := s.StartRun(ctx, "old", "refusalwar", mekbay.Hangar{URL: goodHangarURL, Instance: goodInstance}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, ok, err := s.ClaimHangarEdit(ctx, "old", "refusalwar"); err != nil || !ok {
		t.Fatalf("a run from before claims can be claimed: ok=%v err=%v", ok, err)
	}
}
