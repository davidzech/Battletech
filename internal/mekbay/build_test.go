package mekbay

import (
	"testing"
	"time"
)

// A hangar built on the site is a pristine force in MekBay's save format, which reads back as the units it was built from.
func TestNewHangar(t *testing.T) {
	raw, err := NewHangar(ForceDraft{Name: "Built", Groups: []GroupDraft{{Name: HangarGroup, Units: []UnitDraft{{Unit: "BMKoshi_E", Gunnery: 4, Piloting: 5}, {Unit: "BMKoshi_E", Gunnery: 4, Piloting: 5}}}}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	h, err := ParseForce(raw)
	if err != nil || h.Name != "Built" || len(h.Units) != 2 || h.Units[0].Unit != "BMKoshi_E" || h.Units[0].ID == h.Units[1].ID || h.Units[0].Gunnery != 4 || h.Units[0].Pilot != "" || h.Units[0].Damaged {
		t.Errorf("read back: %+v %v", h, err)
	}
	if _, err := ParseForce(nil); err == nil {
		t.Error("no data is no hangar")
	}
}
