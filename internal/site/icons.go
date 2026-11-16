package site

import (
	"embed"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
)

// The unit icons are MegaMek's (mm-data, CC BY-NC-SA 4.0; see icons/NOTICE.md), vendored by gen_icons.go and embedded
// in the binary, so the site never hotlinks MekBay or GitHub for them.
//
//go:embed icons/*.png
var iconsFS embed.FS

var iconFileName = regexp.MustCompile(`^[a-z0-9._-]+\.png$`)

// iconURL is where a unit's icon is served from: its own icon if one was vendored, else the default icon of its
// weight class.
func iconURL(u Unit) string {
	if u.Icon != "" {
		if _, err := fs.Stat(iconsFS, "icons/"+u.Icon); err == nil {
			return "/icons/" + u.Icon
		}
	}
	return defaultIconURL(u.Class)
}

// defaultIconURL is the generic icon for a weight class (medium if the class is unknown).
func defaultIconURL(class string) string {
	switch c := strings.ToLower(class); c {
	case "light", "medium", "heavy", "assault":
		return "/icons/default_" + c + ".png"
	}
	return "/icons/default_medium.png"
}

// icon serves one of the embedded icons. Only plain lowercase PNG names are accepted, so a request can't reach
// anything else.
func (s *Server) icon(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if !iconFileName.MatchString(name) {
		s.notFound(w, r)
		return
	}
	b, err := iconsFS.ReadFile("icons/" + name)
	if err != nil {
		s.notFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(b)
}
