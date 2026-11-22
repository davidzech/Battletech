package mekbay

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestCreateForceLive makes a force on the real mekbay.com with a throwaway account and reads it back. It runs only when MEKBAY_LIVE_BATTLE is set.
func TestCreateForceLive(t *testing.T) {
	if os.Getenv("MEKBAY_LIVE_BATTLE") == "" {
		t.Skip("set MEKBAY_LIVE_BATTLE=1 to run against mekbay.com")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cl := &Client{}
	id := NewID()
	got, err := cl.CreateForce(ctx, NewID(), id, ForceDraft{Name: "Live built", Groups: []GroupDraft{{Name: "Hangar", Units: []UnitDraft{{Unit: "BMKoshi_E", Gunnery: 4, Piloting: 5}, {Unit: "BMKoshi_E", Gunnery: 4, Piloting: 5}}}}})
	if err != nil || len(got.Units) != 2 {
		t.Fatalf("create: %+v %v", got, err)
	}
	h, err := cl.ReadHangar(ctx, id)
	if err != nil || len(h.Units) != 2 || h.Units[0].Unit != "BMKoshi_E" || h.Units[0].ID == h.Units[1].ID || h.Units[0].Gunnery != 4 || h.Units[0].Pilot != "" {
		t.Errorf("read back: %+v %v", h, err)
	}
}
