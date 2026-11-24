package mekbay

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
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

// MekBay counts a pilot dead when the cockpit is gone, whatever the crew's stored state: the cockpit slot destroyed, or the head with no structure left.
func TestPilotDeadFromTheCockpit(t *testing.T) {
	reply := func(state string) string {
		return `{"action":"forceData","data":{"name":"F","timestamp":"t","groups":[{"name":"G","units":[{"id":"u1","unit":"BMKoshi_E","state":{` + state + `,"crew":[{"name":"Alrek","gunnerySkill":3,"pilotingSkill":4,"hits":1,"state":0}]}}]}]}}`
	}
	for name, c := range map[string]struct {
		state string
		dead  bool
	}{
		"nothing":        {`"x":1`, false},
		"cockpit crit":   {`"crits":[{"name":"Cockpit","loc":"HD","slot":2,"destroyed":1}]`, true},
		"cockpit intact": {`"crits":[{"name":"Cockpit","loc":"HD","slot":2}]`, false},
		"command":        {`"crits":[{"name":"Command Console","loc":"HD","slot":2,"destroyed":1}]`, false},
		"head gone":      {`"locations":{"HD":{"armor":9,"internal":3}}`, true},
		"head hurt":      {`"locations":{"HD":{"armor":9,"internal":2}}`, false},
	} {
		h, err := ParseHangarForce(json.RawMessage(reply(c.state)))
		if err != nil || len(h.Units) != 1 || h.Units[0].PilotDead != c.dead {
			t.Errorf("%s: %+v %v", name, h.Units, err)
		}
	}
}

// setCommander marks one unit of a force as MekBay's commander and takes the mark off every other, even one the hangar copy carried.
func TestSetCommander(t *testing.T) {
	force := decodeForce(t, `{"groups":[{"units":[{"id":"a","commander":true},{"id":"b"}]},{"units":[{"id":"c"}]}]}`)
	marked := func() []string {
		var out []string
		for _, g := range force["groups"].([]any) {
			for _, u := range g.(map[string]any)["units"].([]any) {
				um := u.(map[string]any)
				if um["commander"] == true {
					out = append(out, um["id"].(string))
				}
			}
		}
		return out
	}
	setCommander(force, "c")
	if got := marked(); len(got) != 1 || got[0] != "c" {
		t.Errorf("commander on c only: %v", got)
	}
	setCommander(force, "")
	if got := marked(); len(got) != 0 {
		t.Errorf("an empty unit clears the mark: %v", got)
	}
}

// A force built from a draft (a scenario's own mechs) carries the commander mark as MekBay saves it.
func TestBuildForceMarksTheCommander(t *testing.T) {
	f := BuildForce("i", time.Now(), ForceDraft{Name: "F", Groups: []GroupDraft{{Name: "G", Units: []UnitDraft{{Unit: "BMGrizzly"}, {Unit: "BMLoki_Prime", Commander: true}}}}})
	b, _ := json.Marshal(f)
	if !strings.Contains(string(b), `"unit":"BMLoki_Prime","alias":"","updatedTs":`) || strings.Count(string(b), `"commander":true`) != 1 {
		t.Errorf("one commander, on the Loki: %s", b)
	}
}
