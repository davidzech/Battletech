package layouts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/davidzech/Battletech/internal/mtf"
)

func mtfCanary(src []byte) string { return mtf.Canary(src) }

// A fake MekBay site publishing one changed design file.
func fakeSite(t *testing.T, cur *Table, uuid string, src []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var files atomic.Int32
	eqJSON, _ := mtf.WriteEquipments(cur.Equipments())
	manifest := map[string]string{uuid + ".mtf": mtf.HashBytes(src), "019f583e-a0ac-7f3a-b609-a6a765425c61.blk": "x"}
	mb, _ := json.Marshal(manifest)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == assetsPath:
			_ = json.NewEncoder(w).Encode(map[string]string{"online-assets/generated/units-manifest.json": "new", "online-assets/static/equipment.json": "eq"})
		case r.URL.Path == equipmentPath:
			_, _ = w.Write(eqJSON)
		case r.URL.Path == manifestPath:
			_, _ = w.Write(mb)
		case r.URL.Path == unitDir+uuid+".mtf":
			files.Add(1)
			_, _ = w.Write(src)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return ts, &files
}

func TestSyncFetchesOnlyWhatChanged(t *testing.T) {
	base := Current()
	t.Cleanup(func() { Install(base) })
	// The other designs keep their hash: only the one design changes.
	cur := Build(Empty(), "old", "", base.Equipments(), nil)
	for k, v := range base.byUUID {
		cur.byUUID[k] = v
	}
	src := []byte("Config:Biped\ntechbase:Clan\nmass:20\n\nLeft Arm:\nShoulder\nCLMG\n\n")
	ts, files := fakeSite(t, cur, "019f583e-c5dd-78ba-9fc5-f14baf6df754", src)
	Install(cur)
	if err := Sync(context.Background(), ts.URL); err != nil {
		t.Fatal(err)
	}
	if files.Load() != 1 {
		t.Errorf("fetched %d files, want 1", files.Load())
	}
	now := Current()
	if now.Units != "new" || now.Len() != cur.Len() {
		t.Errorf("synced table: units %q, %d designs", now.Units, now.Len())
	}
	l, _ := Lookup("019f583e-c5dd-78ba-9fc5-f14baf6df754", "")
	if len(l.Mounts) != 1 {
		t.Errorf("the new revision has %d components", len(l.Mounts))
	}
	if old, _ := Lookup("019f583e-c5dd-78ba-9fc5-f14baf6df754", base.byUUID["019f583e-c5dd-78ba-9fc5-f14baf6df754"][0].Canary); len(old.Mounts) != 15 {
		t.Errorf("the embedded revision is still there by its canary: %d components", len(old.Mounts))
	}
	// Up to date: a second sync fetches nothing.
	if err := Sync(context.Background(), ts.URL); err != nil || files.Load() != 1 {
		t.Errorf("second sync: %v, %d files", err, files.Load())
	}
}

// A site still on the release before version 2 serves its web page for the manifest path.
func TestSyncWithoutUnitFiles(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<!doctype html><html></html>"))
	}))
	defer ts.Close()
	before := Current()
	if err := Sync(context.Background(), ts.URL); !errors.Is(err, ErrNoUnitFiles) {
		t.Errorf("Sync = %v, want ErrNoUnitFiles", err)
	}
	if Current() != before {
		t.Error("a failed sync changed the table")
	}
}
