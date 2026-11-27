package site

import (
	"net/url"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/pilot"
)

func TestParseCrewPairs(t *testing.T) {
	pilots := []pilot.NamedPilot{
		{Name: "Alrek", SP: pilot.SPAllocation{Gunnery: 90, Piloting: 100}},
		{Name: "Katarina", SP: pilot.SPAllocation{Gunnery: 90}},
		{Name: "Dead Dan", Fallen: true},
	}
	h := hangarOf("BMMadCat_H", "BMKoshi_E", "BMGrizzly") // ua, ub, uc
	parse := func(pairs ...string) ([]mekbay.CrewAssignment, error) {
		v := url.Values{}
		for i := 0; i+1 < len(pairs); i += 2 {
			v.Add("assign_unit", pairs[i])
			v.Add("assign_pilot", pairs[i+1])
		}
		return parseCrewPairs(v["assign_unit"], v["assign_pilot"], h, pilots)
	}
	got, err := parse("ua", "alrek", "ub", "", "uc", "Katarina")
	if err != nil || len(got) != 3 {
		t.Fatalf("valid: %v %v", got, err)
	}
	if got[0].Name != "Alrek" || got[0].Gunnery != pilots[0].Gunnery() || got[0].Piloting != pilots[0].Piloting() {
		t.Errorf("the pilot's skills come from their levels, with their own spelling of the name: %+v", got[0])
	}
	if got[1].Name != "" || got[1].UnitID != "ub" {
		t.Errorf("an empty pilot clears the unit: %+v", got[1])
	}
	for name, pairs := range map[string][]string{
		"unknown unit":   {"zz", "Alrek"},
		"unknown pilot":  {"ua", "Nobody"},
		"a killed pilot": {"ua", "Dead Dan"},
		"one pilot, two": {"ua", "Alrek", "ub", "alrek"},
		"one unit, two":  {"ua", "Alrek", "ua", "Katarina"},
	} {
		if _, err := parse(pairs...); err == nil {
			t.Errorf("%s should be refused", name)
		}
	}
	if _, err := parseCrewPairs([]string{"ua"}, nil, h, pilots); err == nil {
		t.Error("unit and pilot fields must pair up")
	}
	if got, err := parse(); err != nil || got != nil {
		t.Errorf("no assignments: %v %v", got, err)
	}
}

func TestOverviewCarriesPilotCards(t *testing.T) {
	srv, _, cookie := startedCompany(t)
	page := body(t, srv, "/refusalwar/", cookie)
	for _, want := range []string{`id="force-overview"`, `data-pilots="`, `data-abilities="`, `data-name="Alrek"`, `data-name="Katarina"`, "edgeTokens", "/static/pilotcard.js", "/static/roster.js", "/static/abilitycard.js",
		"/static/campaign.js"} { // campaign.js drops a start wizard fragment left over
		if !strings.Contains(page, want) {
			t.Errorf("the overview is missing %q", want)
		}
	}
}
