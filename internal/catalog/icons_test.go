package catalog

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

func TestEveryMechHasAnIconOrADefault(t *testing.T) {
	loadUnits()
	var mechs, own int
	for _, u := range unitsByName {
		if u.Type != "Mek" {
			continue
		}
		mechs++
		got := IconURL(u)
		if !strings.HasPrefix(got, "/icons/") {
			t.Fatalf("%s: icon URL %q", u.Name, got)
		}
		if _, err := fs.Stat(IconFiles, "icons/"+strings.TrimPrefix(got, "/icons/")); err != nil {
			t.Errorf("%s: icon %s is not in the embedded files", u.Name, got)
		}
		if !strings.Contains(got, "default_") {
			own++
		}
	}
	if mechs < 4000 || own < mechs-25 { // the catalog refresh of 2026-11-16 added a few variants (the "h" retrofits, UrbanMech LAMs) the vendored icon set doesn't have yet
		t.Errorf("%d of %d mechs have their own icon; at most a handful should fall back to a default", own, mechs)
	}
	for _, class := range []string{"Light", "Medium", "Heavy", "Assault", "", "Superheavy"} {
		if _, err := fs.Stat(IconFiles, "icons/"+strings.TrimPrefix(DefaultIconURL(class), "/icons/")); err != nil {
			t.Errorf("default icon for class %q is missing: %v", class, err)
		}
	}
	if DefaultIconURL("Assault") != "/icons/default_assault.png" || DefaultIconURL("") != "/icons/default_medium.png" || DefaultIconURL("heavy") != "/icons/default_heavy.png" {
		t.Error("default icons follow the weight class, with medium as the fallback")
	}
}

func TestIconFileNamesAreWindowsSafe(t *testing.T) {
	// Names differing only by case would clash on a case-insensitive file system, and anything outside this
	// alphabet wouldn't be served by the icon route.
	seen := map[string]string{}
	plain := regexp.MustCompile(`^[a-z0-9._-]+\.png$`)
	entries, err := fs.ReadDir(IconFiles, "icons")
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

// Vehicles and battle armor have icons of their own, named after their folder so they can't clash with a mech's; one without falls back to its type's default.
func TestVehicleAndBattleArmorIcons(t *testing.T) {
	for id, want := range map[string]string{
		"SVSaviorRepairVehicle":              "/icons/vehicles_savior.png",
		"BAElementalBattleArmor_APGaussSqd5": "/icons/battle_armor_elemental_5-co.png",
		"BMKoshi_C":                          "/icons/koshi_c.png",
	} {
		u, ok := ByName(id)
		if !ok {
			t.Fatalf("%s is not in the catalog", id)
		}
		if got := IconURL(u); got != want {
			t.Errorf("%s: %s, want %s", id, got, want)
		}
	}
	for typ, want := range map[string]string{"Tank": "/icons/default_tracked.png", "VTOL": "/icons/default_vtol.png", "Infantry": "/icons/default_ba.png", "Mek": "/icons/default_medium.png"} {
		if got := IconURL(Unit{Type: typ, Class: "Medium"}); got != want {
			t.Errorf("a %s without an icon: %s, want %s", typ, got, want)
		}
		if _, err := IconFiles.Open(strings.TrimPrefix(want, "/")); err != nil {
			t.Errorf("%s is not vendored: %v", want, err)
		}
	}
}
