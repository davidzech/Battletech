package site

import (
	"net/http"

	"github.com/davidzech/Battletech/internal/catalog"
)

// icon serves one of the embedded icons. Only plain lowercase PNG names are accepted, so a request can't reach
// anything else.
func (s *Server) icon(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if !catalog.IconFileName.MatchString(name) {
		s.notFound(w, r)
		return
	}
	b, err := catalog.IconFiles.ReadFile("icons/" + name)
	if err != nil {
		s.notFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(b)
}
