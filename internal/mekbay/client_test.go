package mekbay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const ownerUID = "e66a3544-8fcf-4dab-955a-f4ea2db0b610"

func TestBuildForceIsPristine(t *testing.T) {
	d := ForceDraft{Name: "X - OpFor", Groups: []GroupDraft{{Name: "OpFor 1", Units: []UnitDraft{{Unit: "BMGrizzly", BV: 1500, Pilot: "Drusus", Gunnery: 3, Piloting: 4}}}, {Name: "OpFor 2", Units: []UnitDraft{{Unit: "BMLoki_Prime", BV: 2000}}}}}
	f := BuildForce("force-id", time.Date(2026, 11, 16, 7, 4, 11, 0, time.UTC), d)
	b, _ := json.Marshal(f)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["instanceId"] != "force-id" || m["type"] != "cbt" || m["bv"] != float64(3500) || m["timestamp"] != "2026-11-16T07:04:11.000Z" {
		t.Errorf("force header = %s", b)
	}
	if len(f.Groups) != 2 || f.Groups[0].Name != "OpFor 1" || f.Groups[0].Units[0].Unit != "BMGrizzly" {
		t.Fatalf("groups = %+v", f.Groups)
	}
	st := f.Groups[0].Units[0].State
	if st.Modified || st.Destroyed || len(st.Locations) != 0 || st.Crew[0].Name != "Drusus" || st.Crew[0].Gunnery != 3 {
		t.Errorf("unit state = %+v", st)
	}
	if f.Groups[0].ID == f.Groups[1].ID || f.Groups[0].Units[0].ID == f.Groups[1].Units[0].ID {
		t.Error("groups and units need distinct IDs")
	}
}

func decodeForce(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestParseHangarForce(t *testing.T) {
	raw := json.RawMessage(`{"action":"forceData","data":{"name":"H","timestamp":"t1","groups":[
		{"name":"Deployed","units":[{"id":"a","unit":"BMGrizzly","state":{"locations":{"HD":{"armor":3}},"crew":[{"name":"Anya","gunnerySkill":3,"pilotingSkill":4}]}}]},
		{"name":"Hangar","units":[
			{"id":"b","unit":"BMLoki_Prime","state":{"crits":[{"id":"x","hits":1}],"crew":[{"name":"","gunnerySkill":4,"pilotingSkill":5}]}},
			{"id":"c","unit":"BMUller_J","state":{"destroyed":true,"locations":{}}},
			{"id":"d","unit":"BMUller_J","state":{"crits":[{"id":"y"}],"locations":{}}},
			{"id":"e","unit":"BMUller_J","state":{"crits":[{"id":"z","name":"LRM 5 Ammo","consumed":5}],"locations":{}}}]}]}}`)
	h, err := ParseHangarForce(raw)
	if err != nil || h.Name != "H" || h.Timestamp != "t1" || len(h.Units) != 5 {
		t.Fatalf("parseHangarForce = %+v, %v", h, err)
	}
	a, b, c, d := h.Units[0], h.Units[1], h.Units[2], h.Units[3]
	if a.Group != "Deployed" || a.Pilot != "Anya" || a.Gunnery != 3 || a.Piloting != 4 || !a.Damaged || a.Destroyed {
		t.Errorf("a = %+v", a)
	}
	if !b.Damaged || b.Group != "Hangar" {
		t.Errorf("a crit hit should count as damage: %+v", b)
	}
	if !c.Destroyed || c.Damaged {
		t.Errorf("c = %+v", c)
	}
	if d.Damaged {
		t.Errorf("an untouched crit slot is not damage: %+v", d)
	}
	if e := h.Units[4]; e.Damaged || e.AmmoBins != 1 || e.AmmoUsed != 5 {
		t.Errorf("spent ammunition is Ammo expended, not damage: %+v", e)
	}
	if _, err := ParseHangarForce(json.RawMessage(`{"action":"forceData","data":null}`)); err == nil {
		t.Error("a missing force should be an error")
	}
}

const (
	fakeHangar = `{"instanceId":"` + goodInstance + `","timestamp":"2026-11-16T07:04:11.406Z","name":"Hangar","owned":false,"c3Networks":[],
		"groups":[{"id":"g1","name":"Alpha","units":[{"id":"u1","unit":"BMGrizzly","state":{"crew":[{"name":"A","gunnerySkill":4,"pilotingSkill":5}]}},{"id":"u2","unit":"BMLoki_Prime","state":{}}]}]}`
)

func TestMekBayClientReportsRefusals(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := websocket.Accept(w, r, nil)
		defer c.CloseNow()
		for {
			_, b, err := c.Read(r.Context())
			if err != nil {
				return
			}
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			rid, _ := m["requestId"].(string)
			var out map[string]any
			switch m["action"] {
			case "getForce":
				out = map[string]any{"action": "forceData", "requestId": rid, "data": decodeForce(t, fakeHangar)}
			case "saveForce":
				out = map[string]any{"action": "warning", "requestId": rid, "code": "not_owner", "message": "You do not own this force"}
			default:
				continue
			}
			bb, _ := json.Marshal(out)
			_ = c.Write(r.Context(), websocket.MessageText, bb)
		}
	}))
	defer ts.Close()
	cl := &Client{URL: "ws" + strings.TrimPrefix(ts.URL, "http")}
	_, err := cl.SellUnit(context.Background(), SellRequest{OwnerUID: ownerUID, Instance: goodInstance, UnitID: "u1"})
	if err == nil || !strings.Contains(err.Error(), "doesn't own") {
		t.Fatalf("expected a not-owner error, got %v", err)
	}
}
