package site

import (
	"bytes"
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func unitNamed(t *testing.T, id string) Unit {
	t.Helper()
	u, ok := unitByName(id)
	if !ok {
		t.Fatalf("no unit %s in the catalog", id)
	}
	return u
}

func TestUnitNamesPreferTheClanName(t *testing.T) {
	for _, c := range []struct{ id, name, model string }{
		{"BMVulture_Prime", "Mad Dog", "Prime"},  // Vulture (Mad Dog)
		{"BMKoshi_E", "Mist Lynx", "E"},          // Koshi (Mist Lynx)
		{"BMMadCat_H", "Timber Wolf", "H"},       // Mad Cat (Timber Wolf)
		{"BMLoki_Prime", "Hellbringer", "Prime"}, // Loki (Hellbringer)
		{"BMUller_J", "Kit Fox", "J"},            // Uller (Kit Fox)
		{"BMGrizzly", "Grizzly", ""},             // one name only
		{"BMShadowCat_A", "Shadow Cat", "A"},     // Clan design, one name
	} {
		u := unitNamed(t, c.id)
		if name, model := unitNames(u); name != c.name || model != c.model {
			t.Errorf("%s: unitNames = %q, %q; want %q, %q (chassis %q)", c.id, name, model, c.name, c.model, u.Chassis)
		}
	}
	if got := displayName(unitNamed(t, "BMVulture_Prime")); got != "Mad Dog Prime" {
		t.Errorf("displayName = %q", got)
	}

	// Inner Sphere designs with a parenthesis have no Clan name, so they keep their first name.
	var isWithParens int
	loadUnits()
	for _, u := range unitsByName {
		if u.Type != "Mek" || !chassisNames.MatchString(u.Chassis) || u.TechBase == "Clan" {
			continue
		}
		isWithParens++
		m := chassisNames.FindStringSubmatch(u.Chassis)
		if name, _ := unitNames(u); name != m[1] {
			t.Errorf("%s (%s, %s): shown as %q, want %q", u.Name, u.Chassis, u.TechBase, name, m[1])
		}
	}
	if isWithParens == 0 {
		t.Error("expected a few Inner Sphere designs with a parenthesis (Katana (Crockett))")
	}

	// Across the whole catalog: a Clan design with two names always shows the Clan one, and no mech's shown name
	// still carries a parenthesis.
	for _, u := range unitsByName {
		if u.Type != "Mek" {
			continue
		}
		name, _ := unitNames(u)
		if strings.ContainsAny(name, "()") {
			t.Errorf("%s: shown name %q still has a parenthesis", u.Name, name)
		}
		if m := chassisNames.FindStringSubmatch(u.Chassis); m != nil && u.TechBase == "Clan" && name != m[2] {
			t.Errorf("%s: a Clan design shows %q, want its Clan name %q", u.Name, name, m[2])
		}
	}
}

func TestRoleLabelNeverReadsNone(t *testing.T) {
	loadUnits()
	var noRole int
	for _, u := range unitsByName {
		if u.Type != "Mek" {
			continue
		}
		got := roleLabel(u)
		if got == "" || strings.EqualFold(got, "none") {
			t.Errorf("%s: role label %q", u.Name, got)
		}
		if strings.EqualFold(strings.TrimSpace(u.Role), "none") {
			noRole++
			if want, _, _ := strings.Cut(u.Class, "/"); got != want {
				t.Errorf("%s has no role, so its band should read its weight class %q, got %q", u.Name, want, got)
			}
		}
	}
	if noRole == 0 {
		t.Error("expected some catalog mechs to have the role None")
	}
	if roleLabel(unitNamed(t, "BMVulture_Prime")) != "Missile Boat" {
		t.Error("a real role is shown as it is")
	}
	if got := roleLabel(Unit{Role: "None", Class: "Ultra Light/PA(L)/Exoskeleton"}); got != "Ultra Light" {
		t.Errorf("a long class list is cut to its first part, got %q", got)
	}
	if got := roleLabel(Unit{Role: "", Class: "Heavy"}); got != "Heavy" {
		t.Errorf("a missing role shows the class, got %q", got)
	}
	// No role label is longer than the one mechrow.js is sized for (anything longer is scaled down there).
	for _, u := range unitsByName {
		if u.Type == "Mek" && len(roleLabel(u)) > 12 {
			t.Errorf("%s: role %q is longer than 12 characters; mechrow.js would scale it down", u.Name, roleLabel(u))
		}
	}
}

func TestEveryMechHasAnIconOrADefault(t *testing.T) {
	loadUnits()
	var mechs, own int
	for _, u := range unitsByName {
		if u.Type != "Mek" {
			continue
		}
		mechs++
		got := iconURL(u)
		if !strings.HasPrefix(got, "/icons/") {
			t.Fatalf("%s: icon URL %q", u.Name, got)
		}
		if _, err := fs.Stat(iconsFS, "icons/"+strings.TrimPrefix(got, "/icons/")); err != nil {
			t.Errorf("%s: icon %s is not in the embedded files", u.Name, got)
		}
		if !strings.Contains(got, "default_") {
			own++
		}
	}
	if mechs < 4000 || own < mechs-10 {
		t.Errorf("%d of %d mechs have their own icon; at most a handful should fall back to a default", own, mechs)
	}
	for _, class := range []string{"Light", "Medium", "Heavy", "Assault", "", "Superheavy"} {
		if _, err := fs.Stat(iconsFS, "icons/"+strings.TrimPrefix(defaultIconURL(class), "/icons/")); err != nil {
			t.Errorf("default icon for class %q is missing: %v", class, err)
		}
	}
	if defaultIconURL("Assault") != "/icons/default_assault.png" || defaultIconURL("") != "/icons/default_medium.png" || defaultIconURL("heavy") != "/icons/default_heavy.png" {
		t.Error("default icons follow the weight class, with medium as the fallback")
	}
}

func TestIconRoute(t *testing.T) {
	srv, _ := newTestServer(t)
	r := get(t, srv, "/icons/vulture.png", nil)
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r.Body)
	if r.StatusCode != http.StatusOK || r.Header.Get("Content-Type") != "image/png" || !strings.Contains(r.Header.Get("Cache-Control"), "max-age") ||
		!bytes.HasPrefix(buf.Bytes(), []byte("\x89PNG\r\n\x1a\n")) || buf.Len() < 200 {
		t.Fatalf("vulture.png: %d %q %q (%d bytes)", r.StatusCode, r.Header.Get("Content-Type"), r.Header.Get("Cache-Control"), buf.Len())
	}
	if r := get(t, srv, "/icons/default_heavy.png", nil); r.StatusCode != http.StatusOK {
		t.Errorf("default_heavy.png: %d", r.StatusCode)
	}
	for _, bad := range []string{"nope.png", "VULTURE.PNG", "vulture.jpg", "vulture", "..%2f..%2fgo.mod", url.PathEscape("a/b.png"), "vulture.png%00", ".png", "x y.png"} {
		if r := get(t, srv, "/icons/"+bad, nil); r.StatusCode != http.StatusNotFound && r.StatusCode != http.StatusBadRequest {
			t.Errorf("/icons/%s: got %d, want 404", bad, r.StatusCode)
		}
	}
}

func TestIconFileNamesAreWindowsSafe(t *testing.T) {
	// Names differing only by case would clash on a case-insensitive file system, and anything outside this
	// alphabet wouldn't be served by the icon route.
	seen := map[string]string{}
	plain := regexp.MustCompile(`^[a-z0-9._-]+\.png$`)
	entries, err := fs.ReadDir(iconsFS, "icons")
	if err != nil || len(entries) < 3000 {
		t.Fatalf("icons: %d entries, %v", len(entries), err)
	}
	for _, e := range entries {
		n := e.Name()
		if !plain.MatchString(n) {
			t.Errorf("icon file %q has a name the route would refuse", n)
		}
		if prev, ok := seen[strings.ToLower(n)]; ok {
			t.Errorf("%q and %q differ only by case", prev, n)
		}
		seen[strings.ToLower(n)] = n
	}
}
