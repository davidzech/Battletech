package site

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/pilot"
)

// A force of three mechs: u1 crewed by the named pilot Alrek (3/4), u2 by an unnamed 4/5 crew, u3 by a named pilot with no new assignment.
const crewForce = `{"groups":[{"name":"Hangar","units":[
 {"id":"u1","unit":"BMMadCat_H","state":{"crew":[{"id":0,"name":"Alrek","gunnerySkill":3,"pilotingSkill":4,"hits":1,"state":0}]}},
 {"id":"u2","unit":"BMKoshi_E","state":{"crew":[{"id":0,"name":"","gunnerySkill":4,"pilotingSkill":5,"hits":0,"state":0}]}},
 {"id":"u3","unit":"BMGrizzly","state":{"crew":[{"id":0,"name":"Katarina","gunnerySkill":4,"pilotingSkill":4,"hits":0,"state":0}]}}]}]}`

func crewOf(t *testing.T, force map[string]any, id string) map[string]any {
	t.Helper()
	groups, _ := force["groups"].([]any)
	for _, g := range groups {
		for _, u := range g.(map[string]any)["units"].([]any) {
			um := u.(map[string]any)
			if um["id"] == id {
				return um["state"].(map[string]any)["crew"].([]any)[0].(map[string]any)
			}
		}
	}
	t.Fatalf("no unit %s", id)
	return nil
}

func TestApplyCrew(t *testing.T) {
	load := func() map[string]any {
		var f map[string]any
		if err := json.Unmarshal([]byte(crewForce), &f); err != nil {
			t.Fatal(err)
		}
		return f
	}
	// Moving Alrek from u1 to u2: u2 gets his name and skills, u1 (not mentioned) is cleared, and nothing else about the crew changes.
	f := load()
	if err := applyCrew(f, []CrewAssignment{{UnitID: "u2", Name: "Alrek", Gunnery: 2, Piloting: 3}}); err != nil {
		t.Fatal(err)
	}
	c2, c1, c3 := crewOf(t, f, "u2"), crewOf(t, f, "u1"), crewOf(t, f, "u3")
	if c2["name"] != "Alrek" || c2["gunnerySkill"] != 2 || c2["pilotingSkill"] != 3 {
		t.Errorf("u2: %v", c2)
	}
	if c1["name"] != "" || c1["gunnerySkill"] != defaultGunnery || c1["pilotingSkill"] != defaultPiloting || c1["hits"] != float64(1) {
		t.Errorf("u1 should be an unnamed 4/5 crew again (keeping its other fields): %v", c1)
	}
	if c3["name"] != "Katarina" {
		t.Errorf("a pilot who isn't being moved stays put: %v", c3)
	}
	// The name match ignores case, and a clear empties a named crew.
	f = load()
	if err := applyCrew(f, []CrewAssignment{{UnitID: "u2", Name: "alrek", Gunnery: 3, Piloting: 4}, {UnitID: "u3"}}); err != nil {
		t.Fatal(err)
	}
	if crewOf(t, f, "u1")["name"] != "" || crewOf(t, f, "u3")["name"] != "" || crewOf(t, f, "u2")["name"] != "alrek" {
		t.Errorf("case-insensitive move and clear: %v %v %v", crewOf(t, f, "u1"), crewOf(t, f, "u2"), crewOf(t, f, "u3"))
	}
	// A unit with no crew entry gets one, and an unknown unit is an error.
	var g map[string]any
	_ = json.Unmarshal([]byte(`{"groups":[{"units":[{"id":"x","unit":"BMMadCat_H","state":{"crew":[]}}]}]}`), &g)
	if err := applyCrew(g, []CrewAssignment{{UnitID: "x", Name: "Alrek", Gunnery: 3, Piloting: 4}}); err != nil || crewOf(t, g, "x")["name"] != "Alrek" {
		t.Errorf("a unit without a crew entry: %v", err)
	}
	if err := applyCrew(load(), []CrewAssignment{{UnitID: "nope", Name: "Alrek"}}); err == nil {
		t.Error("an unknown unit should be refused")
	}
	if err := applyCrew(load(), nil); err != nil {
		t.Error("no assignments is a no-op")
	}
}

func TestParseCrew(t *testing.T) {
	pilots := []pilot.NamedPilot{
		{Name: "Alrek", SP: pilot.SPAllocation{Gunnery: 90, Piloting: 100}},
		{Name: "Katarina", SP: pilot.SPAllocation{Gunnery: 90}},
		{Name: "Dead Dan", Fallen: true},
	}
	h := hangarOf("BMMadCat_H", "BMKoshi_E", "BMGrizzly") // ua, ub, uc
	parse := func(pairs ...string) ([]CrewAssignment, error) {
		v := url.Values{}
		for i := 0; i+1 < len(pairs); i += 2 {
			v.Add("assign_unit", pairs[i])
			v.Add("assign_pilot", pairs[i+1])
		}
		r := httptest.NewRequest("POST", "/", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		_ = r.ParseForm()
		return parseCrew(r, h, pilots)
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
	r := httptest.NewRequest("POST", "/", strings.NewReader("assign_unit=ua"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_ = r.ParseForm()
	if _, err := parseCrew(r, h, pilots); err == nil {
		t.Error("unit and pilot fields must pair up")
	}
	if got, err := parse(); err != nil || got != nil {
		t.Errorf("no assignments: %v %v", got, err)
	}
}

func TestOverviewCarriesPilotCards(t *testing.T) {
	srv, _, cookie := startedCompany(t)
	page := body(t, srv, "/refusalwar/", cookie)
	for _, want := range []string{`id="force-overview"`, `data-pilots="`, `data-abilities="`, `data-name="Alrek"`, `data-name="Katarina"`, "edgeTokens", "/static/pilotcard.js", "/static/roster.js", "/static/abilitycard.js"} {
		if !strings.Contains(page, want) {
			t.Errorf("the overview is missing %q", want)
		}
	}
}
