package site

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/davidzech/Battletech/internal/mekbayproxy"
	"github.com/davidzech/Battletech/internal/storage"
)

// A link names its account in the table itself, so it keeps working on a freshly started server (there is no key to lose), and a links table from when
// accounts were sealed under a key is replaced when the store opens.
func TestMekBayLinksSurviveARestart(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/links.db"
	store, err := storage.OpenSQLite(path)
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

	again, err := storage.OpenSQLite(path) // the server restarts
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Host = storage.LinkPrefix + id + ".localhost"
	if acct, status, _ := mekbayproxy.New(again).Account(r); status != 0 || acct != testAccount {
		t.Errorf("the link still acts as its account: %q %d", acct, status)
	}

	// An old table, with the account sealed, is dropped and made afresh.
	if _, err := again.DB().Exec(`DROP TABLE mekbay_links`); err != nil {
		t.Fatal(err)
	}
	if _, err := again.DB().Exec(`CREATE TABLE mekbay_links (id TEXT PRIMARY KEY, playthrough_id TEXT, campaign TEXT, label TEXT, sealed TEXT, ttl_seconds BIGINT, expires_at BIGINT)`); err != nil {
		t.Fatal(err)
	}
	again.Close()
	upgraded, err := storage.OpenSQLite(path)
	if err != nil {
		t.Fatalf("an old links table is replaced: %v", err)
	}
	defer upgraded.Close()
	if has, _ := columnExists(upgraded, "mekbay_links", "account"); !has {
		t.Error("the links table has its account column")
	}
	if _, err := upgraded.EnsureMekBayLink(ctx, "pt", "camp", tableLabel, testAccount, time.Hour, time.Now()); err != nil {
		t.Errorf("links are made in the new table: %v", err)
	}
}

// columnExists says whether a SQLite table has a column.
func columnExists(s *storage.Store, table, column string) (bool, error) {
	var n int
	err := s.DB().QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&n)
	return n > 0, err
}
