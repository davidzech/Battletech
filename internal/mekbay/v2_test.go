package mekbay

import (
	"testing"
)

// The packed forms are MekBay's own, read back from its server: the Timber Wolf A's design UUID and a unit ID.
func TestV2IDs(t *testing.T) {
	const design, packed = "019f583e-c5dd-78ba-9fc5-f14baf6df754", "AZ9YPsXdeLqfxfFLr233VA"
	if p, err := packUUID(design); err != nil || p != packed {
		t.Errorf("packUUID = %q, %v; want %q", p, err, packed)
	}
	if u, err := unpackUUID(packed); err != nil || u != design {
		t.Errorf("unpackUUID = %q, %v; want %q", u, err, design)
	}
	if _, err := unpackUUID("not-a-uuid"); err == nil {
		t.Error("unpackUUID took garbage")
	}
	// A version 1 unit ID (a plain UUID) keeps its value through MekBay's conversion: packed as an opaque ID, it unpacks to itself.
	for _, id := range []string{design, "unit:" + design, "x", "~x", "~~y"} {
		if got := unpackInstanceID(packInstanceID(id)); got != id {
			t.Errorf("instance ID %q round-trips to %q", id, got)
		}
	}
	if got := packInstanceID("unit:" + design); got != "~u"+packed {
		t.Errorf("packInstanceID(unit:...) = %q", got)
	}
	if got := packInstanceID(design); got != "~"+packed {
		t.Errorf("packInstanceID(uuid) = %q", got)
	}
}

func TestParseV2Force(t *testing.T) {
	raw := []byte(`{"data":{"version":2,"type":"cbt","name":"Alpha","instanceId":"f","timestamp":1795854031147,
	 "units":[
	  {"id":"~AZ9YPsXdeLqfxfFLr233VA","uuid":"AZ9YPsXdeLqfxfFLr233VA","crew":[{"id":"p1","name":"Aidan","g":3,"p":4,"health":{"wounds":6}}],
	   "state":{"l":[["f:ct",5],["r:ct",2],["i:lt",3]],"n":[["i:la","blown-off",1]],"s":[["s:rt:2",1]],"a":[["m:m12",4],["m:m13",0]],
	            "c":[["c:m3",{"s":"destroyed"}]],"rC":[["core.torso-crippling","tok","i:lt",3,"failed"]],
	            "p":{"l":[["i:hd",3]],"s":[["s:ct:0",1]]}}},
	  {"id":"~uAaIhPdkrcVSs3bTl7hAMCg","uuid":"AAAAAAAAAAAAAAAAAAAAAA","destroyed":true}],
	 "groups":[{"id":"~g","name":"Star","unitIndices":[0]},{"id":"~h","unitIndices":[1]}],"cbt":{"r":0}}}`)
	h, err := ParseHangarForce(raw)
	if err != nil {
		t.Fatal(err)
	}
	if h.Name != "Alpha" || len(h.Units) != 2 {
		t.Fatalf("force = %+v", h)
	}
	u := h.Units[0]
	if u.ID != "019f583e-c5dd-78ba-9fc5-f14baf6df754" || u.Unit != "BMMadCat_A" || u.Group != "Star" {
		t.Errorf("unit identity = %q %q %q", u.ID, u.Unit, u.Group)
	}
	if u.Pilot != "Aidan" || u.Gunnery != 3 || u.Piloting != 4 || !u.CrewDead {
		t.Errorf("crew = %q %d/%d dead %v", u.Pilot, u.Gunnery, u.Piloting, u.CrewDead)
	}
	if l := u.Locations["CT"]; l.Armor != 7 {
		t.Errorf("CT loss = %+v, want 7 armor (front and rear)", l)
	}
	if !u.Locations["LA"].Gone || u.Locations["LT"].Internal != 3 {
		t.Errorf("locations = %+v", u.Locations)
	}
	// The head's 3 points of structure, pending, are gone: so is the cockpit.
	if u.Locations["HD"].Internal != 3 || !u.CockpitGone || !u.IsDestroyed() {
		t.Errorf("head = %+v, cockpit gone %v", u.Locations["HD"], u.CockpitGone)
	}
	if len(u.Crits) != 3 || u.AmmoBins != 1 || u.AmmoUsed != 4 || !u.Damaged {
		t.Errorf("crits %+v, ammo bins %d used %d", u.Crits, u.AmmoBins, u.AmmoUsed)
	}
	if !u.TorsoCheckFailed || u.TorsoCheckTrigger != "LT" {
		t.Errorf("torso check = %v %q", u.TorsoCheckFailed, u.TorsoCheckTrigger)
	}
	o := h.Units[1]
	if o.ID != "unit:01a2213d-d92b-7154-acdd-b4e5ee100c0a" || o.Unit != "" || o.UUID != "00000000-0000-0000-0000-000000000000" || !o.Destroyed || o.Damaged {
		t.Errorf("second unit = %+v", o)
	}
}
