//go:build ignore

package main

import (
	"regexp"
	"strings"
)

// iconFile turns a unit's icon path in data/units.json (e.g. "meks/Vulture.png") into the flat, lowercase file name
// it is stored under in internal/site/icons: the base name with anything outside a-z 0-9 . _ - replaced by "_".
// Windows file systems ignore case, so names that differ only by case must not exist; gen_icons.go checks for clashes.
// Shared by gen_units.go (which records it in units_index.json) and gen_icons.go (which copies the files).
func iconFile(path string) string {
	base := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		base = path[i+1:]
	}
	return regexp.MustCompile(`[^a-z0-9._-]+`).ReplaceAllString(strings.ToLower(base), "_")
}
