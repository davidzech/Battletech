package mekbay

import (
	"encoding/json"
	"testing"
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
	if c1["name"] != "" || c1["gunnerySkill"] != DefaultGunnery || c1["pilotingSkill"] != DefaultPiloting || c1["hits"] != float64(1) {
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
