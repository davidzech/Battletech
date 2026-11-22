package site

import (
	"html"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/content"
)

// Refusal War suggests Clan Wolf names, and its pages hand them to pilotnames.js.
func TestRefusalWarNamesReachThePages(t *testing.T) {
	c, ok := (content.Library{Root: "../../campaigns"}).Campaign("refusalwar")
	if !ok || len(c.Names.Forces) == 0 || len(c.Names.Male) == 0 || len(c.Names.Female) == 0 {
		t.Fatalf("Refusal War should have its own names: %+v", c.Names)
	}
	srv, _, cookie := startedCompany(t)
	for _, path := range []string{"/refusalwar/manage/"} {
		page := html.UnescapeString(body(t, srv, path, cookie))
		if !strings.Contains(page, `data-names="{"forces":["13th Wolf Guards"`) {
			t.Errorf("%s should carry the campaign's names for pilotnames.js", path)
		}
	}
}
