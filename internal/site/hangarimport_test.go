package site

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/storage"
)

// oldStore is a database as the site left it before hangars lived in it: a run whose hangar is the MekBay force "live-1" (a copy the site kept, so it has a
// source link), with a checkpoint whose frozen copy is the force "frozen-1", and a run imported already by an earlier import, which left its force set.
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

// importSources are the MekBay forces the database still names for the import: runs' hangar_instance and checkpoints' hangar_copy.
func importSources(t *testing.T, store *storage.Store) []string {
	t.Helper()
	rows, err := store.DB().Query(`SELECT hangar_instance FROM campaign_runs WHERE hangar_instance <> '' UNION ALL SELECT hangar_copy FROM campaign_checkpoints WHERE hangar_copy <> ''`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

// The import reads every hangar still only in MekBay, runs' and checkpoints' alike, stores it as it is, and forgets each one's MekBay force (an imported run's
// too); run again it reads nothing.
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
	if left := importSources(t, store); len(left) != 0 {
		t.Errorf("every MekBay force is forgotten once handled: %v", left)
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
	if !cps[0].HasHangar {
		t.Error("the checkpoint has a hangar now")
	}
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

// What trying again can't fix is skipped and marked handled, so the server starts and later starts don't ask MekBay again: a force MekBay no longer has, data
// that isn't a force, and a checkpoint whose snapshot can't take a hangar. The rest is imported.
func TestImportHangarsSkipsWhatCantBeImported(t *testing.T) {
	ctx := context.Background()
	cases := map[string]struct {
		fake  *fakeMekBay
		setup string // SQL run on the old database first
		run   bool   // whether the run's hangar is imported
		cp    bool   // whether the checkpoint's is
	}{
		"a checkpoint's copy MekBay no longer has": {fake: &fakeMekBay{forces: map[string][]byte{"live-1": forceOf(startingHangar())}, gone: map[string]bool{"frozen-1": true}}, run: true},
		"a run's hangar MekBay no longer has":      {fake: &fakeMekBay{forces: map[string][]byte{"frozen-1": forceOf(startingHangar())}, gone: map[string]bool{"live-1": true}}, cp: true},
		"data that isn't a force":                  {fake: &fakeMekBay{forces: map[string][]byte{"live-1": []byte(`null`), "frozen-1": []byte(`{"name":`)}}},
		"a snapshot that can't be read": {fake: &fakeMekBay{forces: map[string][]byte{"live-1": forceOf(startingHangar()), "frozen-1": forceOf(startingHangar())}},
			setup: `UPDATE campaign_checkpoints SET snapshot = '{"campaign_runs":[]}'`, run: true},
	}
	for name, c := range cases {
		store := oldStore(t)
		if c.setup != "" {
			if _, err := store.DB().Exec(c.setup); err != nil {
				t.Fatal(err)
			}
		}
		if err := ImportHangars(ctx, store, c.fake); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if left := importSources(t, store); len(left) != 0 {
			t.Errorf("%s: skipped hangars are marked handled: %v", name, left)
		}
		if _, err := store.Hangar(ctx, "pt", "refusalwar"); (err == nil) != c.run || (!c.run && !errors.Is(err, storage.ErrNoHangar)) {
			t.Errorf("%s: the run's hangar imported %v: %v", name, c.run, err)
		}
		if cps, _ := store.Checkpoints(ctx, "pt", "refusalwar"); cps[0].HasHangar != c.cp {
			t.Errorf("%s: the checkpoint's hangar imported %v", name, c.cp)
		}
		again := &fakeMekBay{err: errors.New("must not be read")}
		if err := ImportHangars(ctx, store, again); err != nil || len(again.reads) != 0 {
			t.Errorf("%s: a second import reads nothing: %v %v", name, again.reads, err)
		}
	}
}

// A failure to reach MekBay (or a timeout) may pass, so it stops the import with an error naming the hangar, and the server doesn't start; what could be read
// stays imported and the rest stays listed for the next start.
func TestImportHangarsStopsWhenMekBayFails(t *testing.T) {
	ctx := context.Background()
	store := oldStore(t)
	err := ImportHangars(ctx, store, &fakeMekBay{err: errors.New("mekbay is down")})
	if err == nil || !strings.Contains(err.Error(), "mekbay is down") || !strings.Contains(err.Error(), "live-1") {
		t.Fatalf("MekBay down: %v", err)
	}
	if _, err := store.Hangar(ctx, "pt", "refusalwar"); !errors.Is(err, storage.ErrNoHangar) {
		t.Errorf("nothing is stored: %v", err)
	}
	if left := importSources(t, store); !slices.Equal(left, []string{"frozen-1", "live-1"}) {
		t.Errorf("both stay to import: %v", left)
	}
}

// A run from before the site kept its own copy of a hangar (no source link) has the player's own force as its hangar, crews and all: the import resets every
// crew to the unnamed 4/5 one, and keeps the damage.
func TestImportHangarsResetsLegacyCrews(t *testing.T) {
	ctx := context.Background()
	store := oldStore(t)
	if _, err := store.DB().Exec(`UPDATE campaign_runs SET source_url = '' WHERE campaign = 'refusalwar'`); err != nil {
		t.Fatal(err)
	}
	live := startingHangar()
	live.Units[0].Pilot, live.Units[0].Gunnery, live.Units[0].Piloting = "Natasha Kerensky", 2, 3
	live.Units[1].Locations, live.Units[1].Damaged = map[string]mekbay.LocationLoss{"CT": {Armor: 4}}, true
	if err := ImportHangars(ctx, store, &fakeMekBay{forces: map[string][]byte{"live-1": forceOf(live), "frozen-1": forceOf(live)}}); err != nil {
		t.Fatal(err)
	}
	h := runHangarOf(t, store, "pt", "refusalwar")
	if u := h.Units[0]; u.Pilot != "" || u.Gunnery != mekbay.DefaultGunnery || u.Piloting != mekbay.DefaultPiloting {
		t.Errorf("the crew is reset: %+v", u)
	}
	if u := h.Units[1]; u.Locations["CT"].Armor != 4 {
		t.Errorf("the damage is kept: %+v", u)
	}
}

// runHangarOf is a run's stored hangar as the site reads it.
func runHangarOf(t *testing.T, store *storage.Store, playthroughID, campaign string) mekbay.HangarForce {
	t.Helper()
	raw, err := store.Hangar(context.Background(), playthroughID, campaign)
	if err != nil {
		t.Fatal(err)
	}
	h, err := mekbay.ParseForce(raw)
	if err != nil {
		t.Fatal(err)
	}
	return h
}
