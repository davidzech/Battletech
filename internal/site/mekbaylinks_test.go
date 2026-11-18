package site

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMekBayLinkIDs(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id := newMekBayLinkID()
		if !validMekBayLinkID(id) || len(id) != mekbayLinkIDLen || len(mekbayLinkPrefix+id) > 63 || seen[id] {
			t.Fatalf("an id is 32 characters of a-z and 2-7, fits a DNS label and is never repeated: %q", id)
		}
		seen[id] = true
	}
	for _, bad := range []string{"", "short", strings.Repeat("a", 33), strings.Repeat("A", 32), strings.Repeat("1", 32), strings.Repeat("a", 31) + "-"} {
		if validMekBayLinkID(bad) {
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
	if err != nil || !validMekBayLinkID(id) {
		t.Fatalf("a link is made: %q %v", id, err)
	}
	got, ok, err := store.MekBayLink(ctx, id, now.Add(time.Hour))
	if err != nil || !ok {
		t.Fatalf("it is found: %v %v", ok, err)
	}
	if got != testAccount {
		t.Errorf("it names the account: %q", got)
	}
	if _, ok, _ := store.MekBayLink(ctx, newMekBayLinkID(), now); ok {
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

// A link names its account in the table itself, so it keeps working on a freshly started server (there is no key to lose), and a links table from when
// accounts were sealed under a key is replaced when the store opens.
func TestMekBayLinksSurviveARestart(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/links.db"
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsurePlaythrough(ctx, "pt"); err != nil {
		t.Fatal(err)
	}
	id, err := store.EnsureMekBayLink(ctx, "pt", "camp", tableLabel, testAccount, time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	store.Close()

	again, err := OpenSQLite(path) // the server restarts
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Host = mekbayLinkPrefix + id + ".localhost"
	if acct, status, _ := newMekBayProxy(again).account(r); status != 0 || acct != testAccount {
		t.Errorf("the link still acts as its account: %q %d", acct, status)
	}

	// An old table, with the account sealed, is dropped and made afresh.
	if _, err := again.db.Exec(`DROP TABLE mekbay_links`); err != nil {
		t.Fatal(err)
	}
	if _, err := again.db.Exec(`CREATE TABLE mekbay_links (id TEXT PRIMARY KEY, playthrough_id TEXT, campaign TEXT, label TEXT, sealed TEXT, ttl_seconds BIGINT, expires_at BIGINT)`); err != nil {
		t.Fatal(err)
	}
	again.Close()
	upgraded, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("an old links table is replaced: %v", err)
	}
	defer upgraded.Close()
	if has, _ := upgraded.columnExists("mekbay_links", "account"); !has {
		t.Error("the links table has its account column")
	}
	if _, err := upgraded.EnsureMekBayLink(ctx, "pt", "camp", tableLabel, testAccount, time.Hour, time.Now()); err != nil {
		t.Errorf("links are made in the new table: %v", err)
	}
}
