package site

import (
	"net/http"

	"github.com/davidzech/Battletech/internal/pilot"
)

// portrait serves one of the embedded portraits. Only plain lowercase PNG names are accepted.
func (s *Server) portrait(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if !pilot.PortraitFileName.MatchString(name) {
		s.notFound(w, r)
		return
	}
	b, err := pilot.PortraitFiles.ReadFile("portraits/" + name)
	if err != nil {
		s.notFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(b)
}
