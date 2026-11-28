package mtf

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestArchive parses every mech in MekBay's unit archive (MTF_ARCHIVE=<units.zip>, MTF_EQUIPMENT=<equipment.json>): each must parse, and every slot of
// a component must point at one.
func TestArchive(t *testing.T) {
	arch, eqPath := os.Getenv("MTF_ARCHIVE"), os.Getenv("MTF_EQUIPMENT")
	if arch == "" || eqPath == "" {
		t.Skip("set MTF_ARCHIVE and MTF_EQUIPMENT")
	}
	f, err := os.Open(eqPath)
	if err != nil {
		t.Fatal(err)
	}
	eq, err := ReadEquipments(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(arch)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	n, unknown := 0, map[string]int{}
	for _, zf := range z.File {
		if !strings.HasSuffix(zf.Name, ".mtf") {
			continue
		}
		rc, _ := zf.Open()
		b := make([]byte, zf.UncompressedSize64)
		_, _ = ioReadFull(rc, b)
		rc.Close()
		l, err := Parse(b, eq)
		if err != nil {
			t.Errorf("%s: %v", zf.Name, err)
			continue
		}
		n++
		for loc, slots := range l.Slots {
			for i, s := range slots {
				if s.Mount > len(l.Mounts) {
					t.Errorf("%s %s:%d points at m%d of %d", zf.Name, loc, i, s.Mount, len(l.Mounts))
				}
			}
		}
		for _, m := range l.Mounts {
			if m.Type == "" {
				unknown[m.Equipment]++
			}
		}
	}
	t.Logf("%d mechs; equipment names the catalog doesn't know: %v", n, unknown)
}

func ioReadFull(r interface{ Read([]byte) (int, error) }, b []byte) (int, error) {
	n := 0
	for n < len(b) {
		k, err := r.Read(b[n:])
		n += k
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// TestArchiveMatchesCatalog compares, design by design, how many components of each equipment each location holds with MekBay's own count in units.json
// (MTF_UNITS=<units.json>), which MekBay derives from the same files.
func TestArchiveMatchesCatalog(t *testing.T) {
	arch, eqPath, unitsPath := os.Getenv("MTF_ARCHIVE"), os.Getenv("MTF_EQUIPMENT"), os.Getenv("MTF_UNITS")
	if arch == "" || eqPath == "" || unitsPath == "" {
		t.Skip("set MTF_ARCHIVE, MTF_EQUIPMENT and MTF_UNITS")
	}
	f, _ := os.Open(eqPath)
	eq, err := ReadEquipments(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	uf, _ := os.ReadFile(unitsPath)
	var cat struct {
		Units []struct {
			UUID string `json:"uuid"`
			Name string `json:"name"`
			Comp []struct {
				ID string `json:"id"`
				Q  int    `json:"q"`
				L  string `json:"l"`
				T  string `json:"t"`
				C  string `json:"c"`
			} `json:"comp"`
		} `json:"units"`
	}
	if err := json.Unmarshal(uf, &cat); err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]int{}
	names := map[string]string{}
	for _, u := range cat.Units {
		m := map[string]int{}
		for _, c := range u.Comp {
			if e := eq.Find(c.ID, "IS"); c.L == "" || c.T == "S" || c.C == "0" || (e != nil && e.Spreadable) {
				continue
			}
			loc, _, _ := strings.Cut(c.L, "/") // a split weapon: its primary location first
			m[c.ID+"@"+loc] += c.Q
		}
		want[u.UUID], names[u.UUID] = m, u.Name
	}
	z, _ := zip.OpenReader(arch)
	defer z.Close()
	checked, bad := 0, 0
	for _, zf := range z.File {
		uuid, ok := strings.CutSuffix(zf.Name, ".mtf")
		w, known := want[uuid]
		if !ok || !known {
			continue
		}
		rc, _ := zf.Open()
		b := make([]byte, zf.UncompressedSize64)
		_, _ = ioReadFull(rc, b)
		rc.Close()
		l, err := Parse(b, eq)
		if err != nil {
			continue
		}
		got := map[string]int{}
		for _, m := range l.Mounts {
			if e := eq.Find(m.Equipment, "IS"); m.Loc == "" || m.Slots == 0 || (e != nil && e.Spreadable) {
				continue
			}
			got[m.Equipment+"@"+m.Loc]++
		}
		checked++
		diff := []string{}
		for k, v := range w {
			if got[k] != v {
				diff = append(diff, fmt.Sprintf("%s want %d got %d", k, v, got[k]))
			}
		}
		for k, v := range got {
			if _, ok := w[k]; !ok {
				diff = append(diff, fmt.Sprintf("%s want 0 got %d", k, v))
			}
		}
		if len(diff) > 0 {
			bad++
			if bad <= 25 {
				sort.Strings(diff)
				t.Logf("%s %s: %v", names[uuid], uuid, diff)
			}
		}
	}
	t.Logf("%d designs checked, %d differ", checked, bad)
}
