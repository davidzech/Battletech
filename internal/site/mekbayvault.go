package site

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// The link vault. A MekBay link (mekbaylinks.go) names a MekBay account, which is a secret: whoever has the account's UUID can change that
// account's forces. The link table therefore stores the account sealed with AES-GCM under a server key, so a leaked database alone reveals no
// account; the proxy opens it only when a link is used. The seed is never stored, so a synthesized account's UUID can't be recomputed later,
// when a player's browser connects from another laptop; sealing it into the link is how the proxy gets it back.

// mekbayVaultAAD binds a sealed value to this purpose, so one sealed for something else can't be replayed as an account.
var mekbayVaultAAD = []byte("battletech mekbay link account v1")

// ErrBadLink means a MekBay link is not valid, has expired or was revoked.
var ErrBadLink = errors.New("This MekBay link is not valid or has expired.")

type mekbayVault struct {
	aead cipher.AEAD
	// sandbox is the shared account a browser opening the proxy directly on a developer's machine acts as (see sandboxAllowed). It is derived
	// from the key, so it is stable for as long as the key is, and it is not a secret: everyone who opens the proxy that way shares it.
	sandbox string
}

// newMekBayVault derives the AES-256 key from any secret string. An empty secret gives a random key, so sealed values last only as long as
// the process (right for development; production sets MEKBAY_LINK_KEY so links survive a restart).
func newMekBayVault(secret string) *mekbayVault {
	var key [32]byte
	if secret == "" {
		if _, err := rand.Read(key[:]); err != nil {
			panic(err) // only fails if the system has no randomness
		}
	} else {
		key = sha256.Sum256([]byte("battletech mekbay link key|" + secret))
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		panic(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return &mekbayVault{aead: aead, sandbox: MekBayUID(hex.EncodeToString(key[:]), "sandbox")}
}

// Seal encrypts an account UUID, with a fresh nonce each time.
func (v *mekbayVault) Seal(account string) (string, error) {
	if _, err := uuid.Parse(account); err != nil {
		return "", errors.New("not a MekBay user ID")
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := v.aead.Seal(nonce, nonce, []byte(strings.ToLower(account)), mekbayVaultAAD)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Open decrypts what Seal made, or returns ErrBadLink.
func (v *mekbayVault) Open(sealed string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(sealed)
	n := v.aead.NonceSize()
	if err != nil || len(raw) < n+v.aead.Overhead() {
		return "", ErrBadLink
	}
	plain, err := v.aead.Open(nil, raw[:n], raw[n:], mekbayVaultAAD)
	if err != nil {
		return "", ErrBadLink
	}
	if _, err := uuid.Parse(string(plain)); err != nil {
		return "", ErrBadLink
	}
	return string(plain), nil
}
