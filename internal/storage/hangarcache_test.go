package storage

import (
	"context"
	"reflect"
	"testing"
)

// hangarCacheStore checks the cache table on both databases, in a campaign of its own (run from storeContract).
func hangarCacheStore(t *testing.T, s *Store, id string) {
	t.Helper()
	ctx := context.Background()
	const camp, inst = "cachecamp", "01a12f9e-188f-7158-ad67-3fc9700ec733"
	if _, _, ok, err := s.CachedHangar(ctx, id, camp, inst); err != nil || ok {
		t.Fatalf("nothing cached yet: %v %v", ok, err)
	}
	h := hangarOf("BMKoshi_E", "BMGrizzly")
	h.Units[1].Pilot, h.Units[1].Damaged = "Alrek", true
	if err := s.SaveHangarCache(ctx, id, camp, inst, h); err != nil {
		t.Fatal(err)
	}
	got, at, ok, err := s.CachedHangar(ctx, id, camp, inst)
	if err != nil || !ok || got.Name != "Test Hangar" || len(got.Units) != 2 || !reflect.DeepEqual(got.Units[1], h.Units[1]) || at.IsZero() {
		t.Fatalf("cached hangar: %+v at %v ok %v err %v", got, at, ok, err)
	}
	// A copy belongs to the hangar link it was read from.
	if _, _, ok, _ := s.CachedHangar(ctx, id, camp, "another-instance"); ok {
		t.Error("a different hangar link must not see the old copy")
	}
	// Saving again replaces it (one row per run), including its link.
	h2 := hangarOf("BMMadCat_H")
	if err := s.SaveHangarCache(ctx, id, camp, "another-instance", h2); err != nil {
		t.Fatal(err)
	}
	if got, _, ok, _ := s.CachedHangar(ctx, id, camp, "another-instance"); !ok || len(got.Units) != 1 || got.Units[0].Unit != "BMMadCat_H" {
		t.Errorf("the copy should have been replaced: %+v", got)
	}
	if _, _, ok, _ := s.CachedHangar(ctx, id, camp, inst); ok {
		t.Error("the replaced copy's old link is gone")
	}
	// Other campaigns are separate, and deleting the run clears its copy.
	if _, _, ok, _ := s.CachedHangar(ctx, id, "othercamp", "another-instance"); ok {
		t.Error("another campaign's cache")
	}
	if _, err := s.DeleteRun(ctx, id, camp); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, _ := s.CachedHangar(ctx, id, camp, "another-instance"); ok {
		t.Error("deleting the run should clear its cached hangar")
	}
}
