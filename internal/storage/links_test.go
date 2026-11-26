package storage

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMekBayLinkIDs(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id := NewLinkID()
		if !ValidLinkID(id) || len(id) != mekbayLinkIDLen || len(LinkPrefix+id) > 63 || seen[id] {
			t.Fatalf("an id is 32 characters of a-z and 2-7, fits a DNS label and is never repeated: %q", id)
		}
		seen[id] = true
	}
	for _, bad := range []string{"", "short", strings.Repeat("a", 33), strings.Repeat("A", 32), strings.Repeat("1", 32), strings.Repeat("a", 31) + "-"} {
		if ValidLinkID(bad) {
			t.Errorf("%q is not an id", bad)
		}
	}
}

func TestMekBayLinkStore(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(t.TempDir() + "/links.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, pt := range []string{"pt", "pt2"} {
		if _, err := store.EnsurePlaythrough(ctx, pt); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	ttl := 10 * 24 * time.Hour

	id, err := store.EnsureMekBayLink(ctx, "pt", "camp", "player-1", testAccount, ttl, now)
	if err != nil || !ValidLinkID(id) {
		t.Fatalf("a link is made: %q %v", id, err)
	}
	got, ok, err := store.MekBayLink(ctx, id, now.Add(time.Hour))
	if err != nil || !ok {
		t.Fatalf("it is found: %v %v", ok, err)
	}
	if got != testAccount {
		t.Errorf("it names the account: %q", got)
	}
	if _, ok, _ := store.MekBayLink(ctx, NewLinkID(), now); ok {
		t.Error("an unknown id is not found")
	}

	// The same label gives the same id; another label, campaign or playthrough gives another.
	if again, err := store.EnsureMekBayLink(ctx, "pt", "camp", "player-1", testAccount, ttl, now.Add(time.Minute)); err != nil || again != id {
		t.Errorf("a link is stable: %q vs %q %v", again, id, err)
	}
	for name, c := range map[string][3]string{"label": {"pt", "camp", "player-2"}, "campaign": {"pt", "camp2", "player-1"}, "playthrough": {"pt2", "camp", "player-1"}} {
		other, err := store.EnsureMekBayLink(ctx, c[0], c[1], c[2], testAccount, ttl, now)
		if err != nil || other == id {
			t.Errorf("another %s is another link: %q %v", name, other, err)
		}
	}

	// A changed account keeps the id (and so the address).
	if same, err := store.EnsureMekBayLink(ctx, "pt", "camp", "player-1", otherAccount, ttl, now); err != nil || same != id {
		t.Fatalf("a changed account keeps the link: %q %v", same, err)
	}
	if got, _, _ = store.MekBayLink(ctx, id, now); got != otherAccount {
		t.Errorf("the link now names the new account: %q", got)
	}

	// Expiry slides forward while the link is used, and the link ends when it isn't.
	use := func(at time.Time) bool {
		t.Helper()
		_, ok, err := store.MekBayLink(ctx, id, at)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !use(now.Add(ttl - time.Hour)) {
		t.Fatal("still valid just before it expires")
	}
	if !use(now.Add(ttl + 5*24*time.Hour)) {
		t.Error("using it slid the expiry forward, so it lasts past the original end")
	}
	if use(now.Add(ttl + 5*24*time.Hour + ttl + time.Hour)) {
		t.Error("a link nobody uses expires")
	}
	var expires int64
	_ = store.db.QueryRow(`SELECT expires_at FROM mekbay_links WHERE id = ?`, id).Scan(&expires)
	// An expired link's label gets a fresh id the next time it is asked for.
	if fresh, err := store.EnsureMekBayLink(ctx, "pt", "camp", "player-1", testAccount, ttl, now.Add(100*24*time.Hour)); err != nil || fresh == id {
		t.Errorf("an expired link is replaced by a new id: %q %v", fresh, err)
	}

	// Revoking.
	live, _ := store.EnsureMekBayLink(ctx, "pt", "camp", "opfor", testAccount, ttl, now.Add(200*24*time.Hour))
	if ok, err := store.RevokeMekBayLink(ctx, "pt", "camp", "opfor"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, ok, _ := store.MekBayLink(ctx, live, now.Add(200*24*time.Hour)); ok {
		t.Error("a revoked link is gone")
	}
	if ok, _ := store.RevokeMekBayLink(ctx, "pt", "camp", "opfor"); ok {
		t.Error("revoking what isn't there says so")
	}
	if _, err := store.EnsureMekBayLink(ctx, "pt", "camp", "bad", "nope", ttl, now); err == nil {
		t.Error("only a real account can be put in a link")
	}

	// Deleting a campaign run takes its links (and only its links) with it.
	keep, _ := store.EnsureMekBayLink(ctx, "pt", "other-camp", "player-1", testAccount, ttl, now.Add(200*24*time.Hour))
	gone, _ := store.EnsureMekBayLink(ctx, "pt", "camp", "player-9", testAccount, ttl, now.Add(200*24*time.Hour))
	if _, err := store.DeleteRun(ctx, "pt", "camp"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.MekBayLink(ctx, gone, now.Add(200*24*time.Hour)); ok {
		t.Error("deleting the run revokes its links")
	}
	if _, ok, _ := store.MekBayLink(ctx, keep, now.Add(200*24*time.Hour)); !ok {
		t.Error("another campaign's links are untouched")
	}
}

// linkRace asks for the same new link from many requests at once: every one gets the same id and none fails.
func linkRace(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	pt := "link-race-" + t.Name()
	t.Cleanup(func() { _, _ = s.db.Exec(s.q(`DELETE FROM playthroughs WHERE id = ?`), pt) })
	if _, err := s.EnsurePlaythrough(ctx, pt); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	for round := 0; round < 20; round++ {
		label := "player-" + string(rune('a'+round))
		const n = 8
		ids, errs := make([]string, n), make([]error, n)
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ids[i], errs[i] = s.EnsureMekBayLink(ctx, pt, "camp", label, testAccount, LinkTTL, now)
			}()
		}
		wg.Wait()
		for i := range ids {
			if errs[i] != nil || !ValidLinkID(ids[i]) || ids[i] != ids[0] {
				t.Fatalf("round %d, request %d: every request gets the one link: %q vs %q, %v", round, i, ids[i], ids[0], errs[i])
			}
		}
	}
}

func TestEnsureMekBayLinkAtOnceSQLite(t *testing.T) {
	s, err := OpenSQLite(t.TempDir() + "/links.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	linkRace(t, s)
}

// TestEnsureMekBayLinkAtOncePostgres runs only when TEST_DATABASE_URL points at a throwaway PostgreSQL database.
func TestEnsureMekBayLinkAtOncePostgres(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	s, err := OpenPostgres(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	linkRace(t, s)
}
