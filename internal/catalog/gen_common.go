//go:build ignore

package main

import (
	"regexp"
	"strings"
)

// iconFile turns a unit's icon path in data/units.json (e.g. "meks/Vulture.png") into the flat, lowercase file name
// it is stored under in internal/catalog/icons: the base name with anything outside a-z 0-9 . _ - replaced by "_". A mech's
// or a default icon keeps its base name ("vulture.png", "default_ba.png"); any other is prefixed with its folder
// ("vehicles/Savior.png" is "vehicles_savior.png"), since a vehicle and a mech can share a file name.
// Windows file systems ignore case, so names that differ only by case must not exist; gen_icons.go checks for clashes.
// Shared by gen_units.go (which records it in units_index.json) and gen_icons.go (which copies the files).
func iconFile(path string) string {
	name := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		name = path[i+1:]
		if dir := path[:i]; dir != "meks" && dir != "defaults" {
			name = dir + "_" + name
		}
	}
	return regexp.MustCompile(`[^a-z0-9._-]+`).ReplaceAllString(strings.ToLower(name), "_")
}

// iconFolders are the mm-data folders whose icons are vendored: mechs, vehicles (tanks and VTOLs), battle armor and the defaults.
var iconFolders = map[string]bool{"meks": true, "vehicles": true, "battle armor": true, "defaults": true}

// vendoredIcon says whether a unit's icon path is one gen_icons.go copies.
func vendoredIcon(path string) bool {
	i := strings.Index(path, "/")
	return i > 0 && iconFolders[path[:i]]
}
