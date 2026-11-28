package layouts

import (
	"bytes"
	"testing"
)

// The embedded table is MekBay's whole set of mechs, laid out.
func TestEmbeddedTable(t *testing.T) {
	tab := Current()
	if tab.Len() < 4000 || tab.Units == "" {
		t.Fatalf("embedded table: %d designs, units %q", tab.Len(), tab.Units)
	}
	l, ok := Lookup("019f583e-c5dd-78ba-9fc5-f14baf6df754", "") // Timber Wolf A
	if !ok {
		t.Fatal("no Timber Wolf A")
	}
	m, ok := l.MountByID("m14")
	if !ok || m.Type != "ammo" || m.Loc != "RT" || m.Shots != 15 {
		t.Errorf("m14 = %+v", m)
	}
	if s, ok := l.SlotAt("CT", 0); !ok || s.System != "Engine" {
		t.Errorf("CT:0 = %+v", s)
	}
}

// A table written and read back is the same table.
func TestTableRoundTrip(t *testing.T) {
	src := Current()
	var lb, eb bytes.Buffer
	if err := src.Write(&lb, &eb); err != nil {
		t.Fatal(err)
	}
	back, err := ReadTable(lb.Bytes(), eb.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if back.Len() != src.Len() || back.Units != src.Units || back.Equipment != src.Equipment {
		t.Fatalf("round trip: %d/%d designs", back.Len(), src.Len())
	}
	if digest(back.Equipments()) != src.Equipment {
		t.Error("the equipment catalog changed in the round trip")
	}
}

// Lookup gives the revision a unit was saved against, by its canary, else the newest.
func TestLookupByCanary(t *testing.T) {
	old := []byte("Config:Biped\ntechbase:Clan\nmass:20\n\nLeft Arm:\nShoulder\nCLERSmallLaser\n\n")
	newer := []byte("Config:Biped\ntechbase:Clan\nmass:20\n\nLeft Arm:\nShoulder\nCLMG\nCLERSmallLaser\n\n")
	eq := Current().Equipments()
	tab := Build(Empty(), "u1", "", eq, []Parsed{{UUID: "x", Hash: "h1", Source: old}})
	tab = Build(tab, "u2", "", eq, []Parsed{{UUID: "x", Hash: "h2", Source: newer}})
	if l, _ := tab.Lookup("x", ""); len(l.Mounts) != 2 {
		t.Errorf("newest has %d components", len(l.Mounts))
	}
	if l, _ := tab.Lookup("x", mtfCanary(old)); len(l.Mounts) != 1 {
		t.Errorf("the old revision by its canary has %d components", len(l.Mounts))
	}
	if _, ok := tab.Lookup("y", ""); ok {
		t.Error("found a design the table doesn't have")
	}
}
