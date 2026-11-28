package mtf

import (
	"os"
	"testing"
)

func testEquipment(t *testing.T) *Equipments {
	t.Helper()
	f, err := os.Open("testdata/equipment.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	eq, err := ReadEquipments(f)
	if err != nil {
		t.Fatal(err)
	}
	return eq
}

func parseFixture(t *testing.T, name string) *Layout {
	t.Helper()
	src, err := os.ReadFile("testdata/" + name + ".mtf")
	if err != nil {
		t.Fatal(err)
	}
	l, err := Parse(src, testEquipment(t))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func wantMount(t *testing.T, l *Layout, id, equipment, loc string, slots int) Mount {
	t.Helper()
	m, ok := l.MountByID(id)
	if !ok || m.Equipment != equipment || m.Loc != loc || m.Slots != slots {
		t.Errorf("%s = %+v (found %v); want %s in %s, %d slots", id, m, ok, equipment, loc, slots)
	}
	return m
}

func wantSlot(t *testing.T, l *Layout, loc string, index int, system string, mount int) {
	t.Helper()
	if s, ok := l.SlotAt(loc, index); !ok || s.System != system || s.Mount != mount {
		t.Errorf("%s:%d = %+v (found %v); want system %q, m%d", loc, index, s, ok, system, mount)
	}
}

// The Timber Wolf A: heat sinks of two slots each, armor and structure spread over the whole mech as one component each, ammunition.
func TestTimberWolfA(t *testing.T) {
	l := parseFixture(t, "timberwolf_a")
	if l.Config != "Biped" || l.TechBase != "Clan" {
		t.Errorf("config %q, tech base %q", l.Config, l.TechBase)
	}
	wantMount(t, l, "m1", "CLDoubleHeatSink", "LA", 2)
	wantMount(t, l, "m3", "CLERPPC", "LA", 2)
	ff := wantMount(t, l, "m4", "Clan Ferro-Fibrous", "LA", 7) // every Ferro-Fibrous row, from LA:8 on
	if ff.Type != "armor" {
		t.Errorf("Ferro-Fibrous type %q", ff.Type)
	}
	wantMount(t, l, "m8", "CLERPPC", "RA", 2)
	wantMount(t, l, "m9", "CLMediumPulseLaser", "LT", 1)
	wantMount(t, l, "m11", "CLMediumPulseLaser", "LT", 1)
	wantMount(t, l, "m12", "Clan Endo Steel", "LT", 7)
	wantMount(t, l, "m13", "CLStreakSRM6", "RT", 2)
	ammo := wantMount(t, l, "m14", "Clan Streak SRM 6 Ammo", "RT", 1)
	if ammo.Type != "ammo" || ammo.Shots != 15 || ammo.Name != "Streak SRM 6 Ammo" {
		t.Errorf("ammo = %+v", ammo)
	}
	wantMount(t, l, "m15", "CLERSmallLaser", "CT", 1)
	if len(l.Mounts) != 15 {
		t.Errorf("%d components, want 15", len(l.Mounts))
	}
	wantSlot(t, l, "CT", 0, "Engine", 0)
	wantSlot(t, l, "CT", 3, "Gyro", 0)
	wantSlot(t, l, "LT", 1, "Engine", 0)
	wantSlot(t, l, "HD", 2, "Cockpit", 0)
	wantSlot(t, l, "HD", 3, "", 4)
	wantSlot(t, l, "LA", 0, "Shoulder", 0)
	wantSlot(t, l, "RT", 4, "", 14)
	wantSlot(t, l, "LA", 10, "", 0)
	if _, ok := l.SlotAt("HD", 6); !ok {
		t.Error("the head's file lists 12 rows: all of them are slots")
	}
}

// The King Crab KGC-0000's AC/20s are ten slots each, split from the arm into the side torso: one component, counted in the torso.
func TestKingCrabSplitWeapons(t *testing.T) {
	l := parseFixture(t, "kingcrab_kgc0000")
	ac := wantMount(t, l, "m1", "Autocannon/20", "LT", 10)
	if ac.Type != "weapon" {
		t.Errorf("AC/20 type %q", ac.Type)
	}
	wantMount(t, l, "m2", "Autocannon/20", "RT", 10)
	wantMount(t, l, "m3", "LRM 15", "LT", 3)
	wantMount(t, l, "m4", "IS Ammo AC/20", "LT", 1)
	wantSlot(t, l, "LA", 11, "", 1)
	wantSlot(t, l, "LT", 0, "", 1)
	wantSlot(t, l, "LT", 1, "", 1)
	wantSlot(t, l, "LT", 2, "", 3)
}

// The Lumberjack's cargo rows are a ton each, one slot a ton: every row is its own component.
func TestLumberjackCargo(t *testing.T) {
	l := parseFixture(t, "lumberjack_lm1a")
	n := 0
	for _, m := range l.Mounts {
		if m.Equipment == "Cargo" {
			n++
			if m.Slots != 1 {
				t.Errorf("cargo %+v", m)
			}
		}
	}
	if n != 8 {
		t.Errorf("%d cargo components, want 8", n)
	}
}

// Rows pack around the placed systems as MegaMek packs them: an empty row closes up, the systems stay.
func TestPackRows(t *testing.T) {
	got := packRows("LT", []string{"XL Engine", "XL Engine", "-Empty-", "LRM 5", "-Empty-", "IS Ammo LRM-5"})
	want := []string{"XL Engine", "XL Engine", "LRM 5", "IS Ammo LRM-5", "-Empty-", "-Empty-", "-Empty-", "-Empty-", "-Empty-", "-Empty-", "-Empty-", "-Empty-"}
	if len(got) != len(want) {
		t.Fatalf("packed %d rows: %q", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("packed = %q", got)
		}
	}
}

func TestParseRow(t *testing.T) {
	r := parseRow("Autocannon/20 (R) (OMNIPOD)#2")
	if r.name != "Autocannon/20" || r.tag != "2" || r.split {
		t.Errorf("tagged row = %+v", r)
	}
	if r := parseRow("Cargo:SIZE:2.5"); r.name != "Cargo" || r.size != 2.5 {
		t.Errorf("sized row = %+v", r)
	}
	if r := parseRow("ISGaussRifle (SPLIT)"); r.name != "ISGaussRifle" || !r.split {
		t.Errorf("split row = %+v", r)
	}
	if r := parseRow("Ammo#x"); r.name != "Ammo" || r.tag != "" {
		t.Errorf("a malformed tag is dropped: %+v", r)
	}
}

func TestParseRefusesNonMech(t *testing.T) {
	if _, err := Parse([]byte("chassis:Demolisher\nmodel:\n"), testEquipment(t)); err == nil {
		t.Error("a file with no critical sections parsed")
	}
}
