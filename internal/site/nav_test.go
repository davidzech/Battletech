package site

import (
	"strings"
	"testing"
)

// The Deploy section links to Manage force with ?back=<scenario>, and Manage's back link then returns to that sortie. Only a real scenario of the campaign counts.
func TestManageBackLinkReturnsToTheSortie(t *testing.T) {
	srv, _, cookie := startedCompany(t)
	sortie := body(t, srv, "/refusalwar/scenario_001/", cookie)
	if !strings.Contains(sortie, `id="manage-link" href="/refusalwar/manage/?back=scenario_001"`) {
		t.Fatalf("the Deploy section should link to Manage force carrying the sortie: %.600s", sortie)
	}
	// A scenario that provides its own mechs has no hangar to manage for it.
	if strings.Contains(body(t, srv, "/refusalwar/scenario_000/", cookie), `id="manage-link"`) {
		t.Error("a scenario with provided mechs should not offer Manage force")
	}
	man := body(t, srv, "/refusalwar/manage/?back=scenario_001", cookie)
	if !strings.Contains(man, `<a href="/refusalwar/scenario_001/">&lsaquo; Back to Sortie 001: `) {
		t.Errorf("Back should return to the sortie: %.400s", man)
	}
	if plain := body(t, srv, "/refusalwar/manage/", cookie); !strings.Contains(plain, `<a href="/refusalwar/">&lsaquo; Back to the campaign</a>`) {
		t.Error("without ?back, Back returns to the campaign")
	}
	for _, bad := range []string{"https://evil.example/", "//evil.example", "../scenario_001", "scenario_999", "scenario_001/../x", "%0d%0a"} {
		page := body(t, srv, "/refusalwar/manage/?back="+bad, cookie)
		if !strings.Contains(page, `<a href="/refusalwar/">&lsaquo; Back to the campaign</a>`) || strings.Contains(page, "evil.example") {
			t.Errorf("?back=%s must be ignored", bad)
		}
	}
}
