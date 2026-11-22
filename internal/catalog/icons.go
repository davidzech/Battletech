package catalog

import (
	"embed"
	_ "embed"
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

// IconURL is where a unit's icon is served from: its own icon if one was vendored, else the default icon of its
// weight class.
func IconURL(u Unit) string {
	if u.Icon != "" {
		if _, err := fs.Stat(IconFiles, "icons/"+u.Icon); err == nil {
			return "/icons/" + u.Icon
		}
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
