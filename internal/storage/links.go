package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"errors"
	"strings"
	"time"
	"uuid"

	"github.com/davidzech/Battletech/internal/mekbay"
)

// MekBay links: what makes a browser act as a MekBay account (see mekbayproxy.go). A link is a long random id that appears as the first label of
// the host name the proxy is reached at, https://mekbay-<id>.<site host>/, and names one account: a player's, the OpFor's or the site's hangar
// copy. The address is the identity, so a link can be sent in a message and opens as that account with nothing else to do, and closing a tab,
// bookmarking or opening it in two tabs next to another link all work, because every link is its own browser origin with its own cookies and
// storage.
//
// The id is a bearer credential, so it is 160 random bits, it lives in a table beside the account it names, it expires (and
// the expiry slides forward while the link is in use) and it can be revoked. The proxy marks every response Referrer-Policy: no-referrer, so
// following a link out of MekBay never reveals the host name.
//
// There is one link per playthrough, campaign and label (such as "hangar" or "opfor"), so the same link is handed out each time and can be
// bookmarked; revoking it makes the next request for that label mint a new id (and so a new host name).

const (
	// LinkPrefix starts the first label of a link's host name: mekbay-<id>.
	LinkPrefix = "mekbay-"
	// mekbayLinkIDLen is the length of an id: 20 random bytes in lowercase base32.
	mekbayLinkIDLen = 32
	// LinkTTL is how long a link stays valid after it was last handed out or used.
	LinkTTL = 14 * 24 * time.Hour
)

var linkEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewLinkID makes a link id: 160 random bits as 32 characters of a-z and 2-7, which are safe in a DNS label.
func NewLinkID() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic(err) // only fails if the system has no randomness
	}
	return strings.ToLower(linkEncoding.EncodeToString(b))
}

// ValidLinkID says whether s has the shape of a link id (it says nothing about whether the link exists).
func ValidLinkID(s string) bool {
	if len(s) != mekbayLinkIDLen {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '2' && c <= '7') {
			return false
		}
	}
	return true
}

// ErrBadLink means a MekBay link is not valid, has expired or was revoked.
var ErrBadLink = errors.New("This MekBay link is not valid or has expired.")

// SandboxAccount is the shared account a browser opening the proxy directly on a developer's machine acts as (see sandboxAllowed). It is not a secret: everyone
// who opens the proxy that way shares it.
var SandboxAccount = mekbay.MekBayUID("battletech localhost sandbox", "sandbox")

// EnsureMekBayLink returns the link id for a playthrough's campaign and label, making it when there is none and making sure it names account and
// is valid for ttl from now. Asking again gives the same id, so the link stays the same (and bookmarkable) for as long as it lives.
func (s *Store) EnsureMekBayLink(ctx context.Context, playthroughID, campaign, label, account string, ttl time.Duration, now time.Time) (string, error) {
	if _, err := uuid.Parse(account); err != nil {
		return "", errors.New("not a MekBay user ID")
	}
	account = strings.ToLower(account)
	expires, secs := now.Add(ttl).Unix(), int64(ttl/time.Second)
	// Expired links are gone for good; clearing them keeps the table small and frees their labels for fresh ids.
	if _, err := s.db.ExecContext(ctx, s.q(`DELETE FROM mekbay_links WHERE expires_at <= ?`), now.Unix()); err != nil {
		return "", err
	}
	// One statement makes the link or refreshes the one that is there, so two requests asking for a new label at once both get the same id
	// (the id offered by the one that loses is simply not used).
	var id string
	err := s.db.QueryRowContext(ctx,
		s.q(`INSERT INTO mekbay_links (id, playthrough_id, campaign, label, account, ttl_seconds, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		     ON CONFLICT (playthrough_id, campaign, label) DO UPDATE SET account = excluded.account, ttl_seconds = excluded.ttl_seconds, expires_at = excluded.expires_at
		     RETURNING id`),
		NewLinkID(), playthroughID, campaign, label, account, secs, expires).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

// MekBayLink looks a link up by id. It reports the link's account when the link exists and has not expired, and slides the expiry forward
// (once a tenth of the lifetime has passed, so a busy link doesn't write on every connection).
func (s *Store) MekBayLink(ctx context.Context, id string, now time.Time) (account string, ok bool, err error) {
	var expires, ttl int64
	err = s.db.QueryRowContext(ctx, s.q(`SELECT account, expires_at, ttl_seconds FROM mekbay_links WHERE id = ?`), id).Scan(&account, &expires, &ttl)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if expires <= now.Unix() {
		return "", false, nil
	}
	if expires-now.Unix() < ttl*9/10 {
		if _, err := s.db.ExecContext(ctx, s.q(`UPDATE mekbay_links SET expires_at = ? WHERE id = ?`), now.Unix()+ttl, id); err != nil {
			return "", false, err
		}
	}
	return account, true, nil
}

// RevokeMekBayLink deletes a link, so its host name stops working at once. Asking for the label again makes a new id.
func (s *Store) RevokeMekBayLink(ctx context.Context, playthroughID, campaign, label string) (bool, error) {
	res, err := s.db.ExecContext(ctx, s.q(`DELETE FROM mekbay_links WHERE playthrough_id = ? AND campaign = ? AND label = ?`), playthroughID, campaign, label)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
