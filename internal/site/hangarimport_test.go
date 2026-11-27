package site

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/storage"
)

// oldStore is a database as the site left it before hangars lived in it: a run whose hangar is the MekBay force "live-1", with a checkpoint whose frozen copy is
// the force "frozen-1", and a run whose import is done already.
func oldStore(t *testing.T) *storage.Store {
	t.Helper()
	ctx := context.Background()
	store, err := storage.OpenSQLite(t.TempDir() + "/old.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	for _, stmt := range []string{
		`INSERT INTO playthroughs (id, company_name) VALUES ('pt', 'Wolf Pack')`,
		`INSERT INTO campaign_runs (playthrough_id, campaign, hangar_url, hangar_instance, source_url) VALUES ('pt', 'refusalwar', 'u', 'live-1', 'src')`,
		`INSERT INTO campaign_runs (playthrough_id, campaign, hangar_url, hangar_instance, hangar) VALUES ('pt', 'done', 'u', 'live-2', '{"name":"done"}')`,
	} {
		if _, err := store.DB().Exec(stmt); err != nil {
			t.Fatal(stmt, err)
		}
	}
	if err := store.AddCheckpoint(ctx, "pt", "refusalwar", "Campaign started"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`UPDATE campaign_checkpoints SET hangar_copy = 'frozen-1'`); err != nil {
		t.Fatal(err)
	}
	return store
}

// The import reads every hangar still only in MekBay, runs' and checkpoints' alike, stores it as it is, and skips what is imported already; run again it
// reads nothing.
func TestImportHangars(t *testing.T) {
	ctx := context.Background()
	store := oldStore(t)
	live, frozen := startingHangar(), hangarOf("BMGrizzly")
	live.Units[0].Locations, live.Units[0].Damaged = nil, true // damage is kept as it is
	fake := &fakeMekBay{forces: map[string][]byte{"live-1": forceOf(live), "frozen-1": forceOf(frozen)}}
	if err := ImportHangars(ctx, store, fake); err != nil {
		t.Fatal(err)
	}
	slices.Sort(fake.reads)
	if !slices.Equal(fake.reads, []string{"frozen-1", "live-1"}) {
		t.Errorf("each hangar still in MekBay is read once, and only those: %v", fake.reads)
	}
	raw, err := store.Hangar(ctx, "pt", "refusalwar")
	if err != nil || string(raw) != string(forceOf(live)) {
		t.Errorf("the run's hangar is stored as MekBay had it: %s %v", raw, err)
	}
	if raw, _ := store.Hangar(ctx, "pt", "done"); string(raw) != `{"name":"done"}` {
		t.Errorf("an imported run is left alone: %s", raw)
	}
	// The checkpoint holds its frozen hangar now: rolling back to it restores that.
	cps, _ := store.Checkpoints(ctx, "pt", "refusalwar")
	if err := store.RestoreCheckpoint(ctx, "pt", "refusalwar", cps[0].Seq); err != nil {
		t.Fatal(err)
	}
	if raw, _ := store.Hangar(ctx, "pt", "refusalwar"); string(raw) != string(forceOf(frozen)) {
		t.Errorf("the checkpoint's hangar: %s", raw)
	}

	again := &fakeMekBay{err: errors.New("must not be read")}
	if err := ImportHangars(ctx, store, again); err != nil || len(again.reads) != 0 {
		t.Errorf("a second import reads nothing: %v %v", again.reads, err)
	}
}

// A hangar that can't be read stops the import with an error naming it, so the server doesn't start; what could be read stays imported.
func TestImportHangarsFailsOnARead(t *testing.T) {
	ctx := context.Background()
	store := oldStore(t)
	fake := &fakeMekBay{forces: map[string][]byte{"live-1": forceOf(startingHangar())}}
	fake.forces["frozen-1"] = []byte(`{"name":`) // not a force
	err := ImportHangars(ctx, store, fake)
	if err == nil || !strings.Contains(err.Error(), "checkpoint 1 of campaign refusalwar") || !strings.Contains(err.Error(), "frozen-1") {
		t.Fatalf("a hangar that can't be read: %v", err)
	}
	if _, err := store.Hangar(ctx, "pt", "refusalwar"); err != nil {
		t.Errorf("the run's hangar, read before the failure, stays imported: %v", err)
	}
	if left, _ := store.CheckpointHangarImports(ctx); len(left) != 1 {
		t.Errorf("the checkpoint is still to import: %+v", left)
	}

	down := &fakeMekBay{err: errors.New("mekbay is down")}
	store2 := oldStore(t)
	if err := ImportHangars(ctx, store2, down); err == nil || !strings.Contains(err.Error(), "mekbay is down") || !strings.Contains(err.Error(), "live-1") {
		t.Errorf("MekBay down: %v", err)
	}
	if _, err := store2.Hangar(ctx, "pt", "refusalwar"); !errors.Is(err, storage.ErrNoHangar) {
		t.Errorf("nothing is stored: %v", err)
	}
}
