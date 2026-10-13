package site

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"regexp"
)

// A seed is the player's only credential: whoever knows it can read and change that playthrough.
// It is restricted to lowercase letters and digits so it's easy to type, say aloud and copy.
const (
	seedCookie = "seed"
	seedMin    = 8
	seedMax    = 32
)

var seedPattern = regexp.MustCompile(`^[a-z0-9]{8,32}$`)

func validSeed(s string) bool { return seedPattern.MatchString(s) }

// playthroughID expands a seed into the opaque ID used as the database key, so the seed itself is
// never stored. The seed is the credential, so pick a long one; the random generator makes 12
// characters (about 62 bits).
func playthroughID(seed string) string {
	sum := sha256.Sum256([]byte("battletech-playthrough-v1:" + seed))
	return hex.EncodeToString(sum[:])
}

// seedHint is the short form shown in the header, e.g. "ab12…".
func seedHint(seed string) string {
	if len(seed) <= 4 {
		return seed
	}
	return seed[:4] + "…"
}

// currentSeed returns the seed in the request's cookie, or "" if absent or malformed.
func currentSeed(r *http.Request) string {
	c, err := r.Cookie(seedCookie)
	if err != nil || !validSeed(c.Value) {
		return ""
	}
	return c.Value
}

func setSeedCookie(w http.ResponseWriter, r *http.Request, seed string) {
	http.SetCookie(w, &http.Cookie{
		Name:     seedCookie,
		Value:    seed,
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60,
		HttpOnly: true, // the server renders the seed into the page; scripts never read the cookie
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
}

// sameOrigin rejects cross-site form posts: a request carrying an Origin header must come from this host.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}
