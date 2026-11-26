package catalog

import (
	"embed"
	"io/fs"
	"regexp"
	"strings"
)

// The unit icons are MegaMek's (mm-data, CC BY-NC-SA 4.0; see icons/NOTICE.md), vendored by gen_icons.go and embedded
// in the binary, so the site never hotlinks MekBay or GitHub for them.
//
//go:embed icons/*.png
var IconFiles embed.FS

var IconFileName = regexp.MustCompile(`^[a-z0-9._-]+\.png$`)

// IconURL is where a unit's icon is served from: its own icon if one was vendored, else a default one: a vehicle's, a VTOL's or battle armor's for those, a
// mech's weight class for anything else.
func IconURL(u Unit) string {
	if u.Icon != "" {
		if _, err := fs.Stat(IconFiles, "icons/"+u.Icon); err == nil {
			return "/icons/" + u.Icon
		}
	}
	switch u.Type {
	case "Tank":
		return "/icons/default_tracked.png"
	case "VTOL":
		return "/icons/default_vtol.png"
	case "Infantry":
		return "/icons/default_ba.png"
	}
	return DefaultIconURL(u.Class)
}

// DefaultIconURL is the generic icon for a weight class (medium if the class is unknown).
func DefaultIconURL(class string) string {
	switch c := strings.ToLower(class); c {
	case "light", "medium", "heavy", "assault":
		return "/icons/default_" + c + ".png"
	}
	return "/icons/default_medium.png"
}
