package site

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MekBay link tokens. The MekBay proxy (mekbayproxy.go) acts as one MekBay account per browser, and the browser learns which one from a
// token carried by the link the site hands out (?t=...). A token is the account's UUID and an expiry sealed with AES-GCM under a server
// key, so it is opaque (the link never shows the UUID, which is a secret: whoever has it can change that account's forces), can't be forged
// or altered, and expires on its own. The server needs to store nothing, which matters because the seed is never stored and so a synthesized
// account's UUID can't be recomputed later, when the player's browser connects from another laptop.
//
// The only revocation is the expiry (and rotating the key, which kills every token at once), so links are short-lived.

// mekbayTokenAAD binds a token to this purpose, so a sealed value from elsewhere can't be replayed as one.
var mekbayTokenAAD = []byte("battletech mekbay link v1")

// ErrBadToken means a MekBay link token is malformed, forged or expired.
var ErrBadToken = errors.New("This MekBay link is not valid or has expired.")

type mekbayTokens struct{ aead cipher.AEAD }

// newMekBayTokens derives the AES-256 key from any secret string. An empty secret gives a random key, so tokens last only as long as the
// process (right for development; production sets MEKBAY_TOKEN_KEY so links survive a restart).
func newMekBayTokens(secret string) *mekbayTokens {
	var key [32]byte
	if secret == "" {
		if _, err := rand.Read(key[:]); err != nil {
			panic(err) // only fails if the system has no randomness
		}
	} else {
		key = sha256.Sum256([]byte("battletech mekbay token key|" + secret))
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		panic(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return &mekbayTokens{aead: aead}
}

// Issue seals an account UUID into a token valid for ttl.
func (t *mekbayTokens) Issue(account string, ttl time.Duration, now time.Time) (string, error) {
	if _, err := uuid.Parse(account); err != nil {
		return "", errors.New("not a MekBay user ID")
	}
	nonce := make([]byte, t.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	plain := []byte(strings.ToLower(account) + "|" + strconv.FormatInt(now.Add(ttl).Unix(), 10))
	sealed := t.aead.Seal(nonce, nonce, plain, mekbayTokenAAD)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Open returns the account a token names and when it expires, or ErrBadToken.
func (t *mekbayTokens) Open(token string, now time.Time) (account string, expires time.Time, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) < t.aead.NonceSize()+t.aead.Overhead() {
		return "", time.Time{}, ErrBadToken
	}
	n := t.aead.NonceSize()
	plain, err := t.aead.Open(nil, raw[:n], raw[n:], mekbayTokenAAD)
	if err != nil {
		return "", time.Time{}, ErrBadToken
	}
	id, exp, ok := strings.Cut(string(plain), "|")
	secs, perr := strconv.ParseInt(exp, 10, 64)
	if !ok || perr != nil {
		return "", time.Time{}, ErrBadToken
	}
	if _, err := uuid.Parse(id); err != nil {
		return "", time.Time{}, ErrBadToken
	}
	expires = time.Unix(secs, 0)
	if !now.Before(expires) {
		return "", time.Time{}, ErrBadToken
	}
	return id, expires, nil
}
