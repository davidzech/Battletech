package site

import (
	"strings"
	"testing"
)

// Refusal War calls its scenarios sorties, and that name is used everywhere a scenario is named.
func TestMissionTerm(t *testing.T) {
	c := refusalWar(t)
	sc, _ := c.Scenario("scenario_001")
	if c.MissionNumber(*sc) != "Sortie 001" || c.MissionName(*sc) != "Sortie 001: Pyre Light" || operationName(c, sc) != "Refusal War - Sortie 001" {
		t.Errorf("names: %q %q %q", c.MissionNumber(*sc), c.MissionName(*sc), operationName(c, sc))
	}
	srv, _, cookie := startedCompany(t)
	if p := body(t, srv, "/refusalwar/scenario_001/", cookie); !strings.Contains(p, `<p class="kicker">Sortie 001`) {
		t.Errorf("the scenario page's kicker: %.300s", p)
	}
	if p := body(t, srv, "/refusalwar/manage/?back=scenario_001", cookie); !strings.Contains(p, "Back to Sortie 001: Pyre Light") {
		t.Error("the Manage page's back link names the sortie")
	}
}
