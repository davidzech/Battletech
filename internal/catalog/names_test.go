package catalog

import (
	"strings"
	"testing"
)

func unitNamed(t *testing.T, id string) Unit {
	t.Helper()
	u, ok := ByName(id)
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
		if name, model := Names(u); name != c.name || model != c.model {
			t.Errorf("%s: unitNames = %q, %q; want %q, %q (chassis %q)", c.id, name, model, c.name, c.model, u.Chassis)
		}
	}
	if got := DisplayName(unitNamed(t, "BMVulture_Prime")); got != "Mad Dog Prime" {
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
		if name, _ := Names(u); name != m[1] {
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
		name, _ := Names(u)
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
		got := RoleLabel(u)
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
	if RoleLabel(unitNamed(t, "BMVulture_Prime")) != "Missile Boat" {
		t.Error("a real role is shown as it is")
	}
	if got := RoleLabel(Unit{Role: "None", Class: "Ultra Light/PA(L)/Exoskeleton"}); got != "Ultra Light" {
		t.Errorf("a long class list is cut to its first part, got %q", got)
	}
	if got := RoleLabel(Unit{Role: "", Class: "Heavy"}); got != "Heavy" {
		t.Errorf("a missing role shows the class, got %q", got)
	}
	// No role label is longer than the one mechrow.js is sized for (anything longer is scaled down there).
	for _, u := range unitsByName {
		if u.Type == "Mek" && len(RoleLabel(u)) > 12 {
			t.Errorf("%s: role %q is longer than 12 characters; mechrow.js would scale it down", u.Name, RoleLabel(u))
		}
	}
}
