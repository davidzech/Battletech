// Package layouts knows every mech design's layout (its critical slots and MekBay's numbering of its components, see package mtf), which reading
// MekBay's version 2 forces needs.
//
// The layouts come from MekBay's own unit files (online-assets/generated/units, the files MekBay's next release reads). A table built from them is
// checked in and embedded (layouts.json.gz, with the equipment catalog it was read with, equipment.json.gz; go generate ./internal/layouts rebuilds both),
// and Sync brings it up to date in memory from MekBay's current files whenever the server starts and once a day: only the files that changed are fetched.
// Every replica does this on its own; nothing is stored.
//
// A design may have more than one revision at hand (the embedded one and a newer one): MekBay stamps each saved unit with a canary of the revision it
// was saved against (mtf.Canary), and Lookup gives the revision with that canary, else the newest.
package layouts

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/davidzech/Battletech/internal/mtf"
)

//go:generate go run gen_layouts.go

//go:embed layouts.json.gz
var embeddedLayouts []byte

//go:embed equipment.json.gz
var embeddedEquipment []byte

// Revision is one revision of a design's layout.
type Revision struct {
	Hash   string // MekBay's hash of the file (units-manifest.json)
	Canary string // MekBay's canary of the file (mtf.Canary), as its saved units carry it
	Layout *mtf.Layout
}

// Table is every design's layouts, by design UUID, newest revision first.
type Table struct {
	Units     string // the hash of the units manifest the table was built from ("" when unknown)
	Equipment string // the equipment catalog's digest (see digest)
	EqFile    string // MekBay's hash of the equipment catalog file it was read from ("" when unknown)
	eq        *mtf.Equipments
	byUUID    map[string][]Revision
}

// Equipments is the equipment catalog the table's layouts were read with.
func (t *Table) Equipments() *mtf.Equipments { return t.eq }

// Len is the number of designs in the table.
func (t *Table) Len() int { return len(t.byUUID) }

// Lookup finds a design's layout: the revision whose canary is canary, else the newest. ok is false when the table has no layout for the design.
func (t *Table) Lookup(uuid, canary string) (*mtf.Layout, bool) {
	revs := t.byUUID[uuid]
	if len(revs) == 0 {
		return nil, false
	}
	if canary != "" {
		for _, r := range revs {
			if r.Canary == canary {
				return r.Layout, true
			}
		}
	}
	return revs[0].Layout, true
}

// Newest is a design's newest revision.
func (t *Table) Newest(uuid string) (Revision, bool) {
	revs := t.byUUID[uuid]
	if len(revs) == 0 {
		return Revision{}, false
	}
	return revs[0], true
}

var (
	current  atomic.Pointer[Table]
	loadOnce sync.Once
)

// Current is the table in use: the embedded one until Sync brings a newer one.
func Current() *Table {
	loadOnce.Do(func() {
		if current.Load() != nil {
			return
		}
		t, err := ReadTable(embeddedLayouts, embeddedEquipment)
		if err != nil {
			log.Printf("layouts: the embedded table is unreadable: %v", err)
			t = &Table{eq: mtf.NewEquipments(nil), byUUID: map[string][]Revision{}}
		}
		current.CompareAndSwap(nil, t)
	})
	return current.Load()
}

// Install makes t the table in use (tests, and Sync).
func Install(t *Table) { loadOnce.Do(func() {}); current.Store(t) }

// Lookup finds a design's layout in the table in use (see Table.Lookup).
func Lookup(uuid, canary string) (*mtf.Layout, bool) { return Current().Lookup(uuid, canary) }

// The stored form of a table (layouts.json.gz): compact, since it holds thousands of designs.
type storedTable struct {
	Units     string                `json:"units"`
	Equipment string                `json:"equipment"`
	EqFile    string                `json:"equipmentFile"`
	Designs   map[string]storedUnit `json:"designs"`
}

type storedUnit struct {
	Hash     string           `json:"h"`
	Canary   string           `json:"k"`
	Config   string           `json:"c,omitempty"` // "" for Biped
	TechBase string           `json:"t,omitempty"` // "" for IS
	Slots    map[string][]any `json:"s"`           // per location: 0 for an empty slot, the component's number, or the system's name
	Mounts   [][]any          `json:"m"`           // [equipment, location, slots]
}

// ReadTable reads a stored table and the equipment catalog it was built with (both gzipped).
func ReadTable(layoutsGz, equipmentGz []byte) (*Table, error) {
	eqJSON, err := gunzip(equipmentGz)
	if err != nil {
		return nil, fmt.Errorf("equipment: %w", err)
	}
	eq, err := mtf.ReadEquipments(bytes.NewReader(eqJSON))
	if err != nil {
		return nil, err
	}
	raw, err := gunzip(layoutsGz)
	if err != nil {
		return nil, fmt.Errorf("layouts: %w", err)
	}
	var st storedTable
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("layouts: %w", err)
	}
	t := &Table{Units: st.Units, Equipment: st.Equipment, EqFile: st.EqFile, eq: eq, byUUID: make(map[string][]Revision, len(st.Designs))}
	for uuid, su := range st.Designs {
		l := &mtf.Layout{Config: su.Config, TechBase: su.TechBase, Slots: map[string][]mtf.Slot{}}
		if l.Config == "" {
			l.Config = "Biped"
		}
		if l.TechBase == "" {
			l.TechBase = "IS"
		}
		for loc, slots := range su.Slots {
			out := make([]mtf.Slot, len(slots))
			for i, s := range slots {
				switch v := s.(type) {
				case float64:
					out[i].Mount = int(v)
				case string:
					out[i].System = v
				}
			}
			l.Slots[loc] = out
		}
		for _, m := range su.Mounts {
			if len(m) != 3 {
				return nil, fmt.Errorf("layouts: design %s has a malformed component", uuid)
			}
			name, _ := m[0].(string)
			loc, _ := m[1].(string)
			n, _ := m[2].(float64)
			l.Mounts = append(l.Mounts, eq.Describe(name, loc, int(n)))
		}
		t.byUUID[uuid] = []Revision{{Hash: su.Hash, Canary: su.Canary, Layout: l}}
	}
	return t, nil
}

// Write stores a table and its equipment catalog (each gzipped), keeping each design's newest revision.
func (t *Table) Write(layouts, equipment io.Writer) error {
	st := storedTable{Units: t.Units, Equipment: t.Equipment, EqFile: t.EqFile, Designs: make(map[string]storedUnit, len(t.byUUID))}
	for uuid, revs := range t.byUUID {
		r := revs[0]
		su := storedUnit{Hash: r.Hash, Canary: r.Canary, Slots: map[string][]any{}}
		if r.Layout.Config != "Biped" {
			su.Config = r.Layout.Config
		}
		if r.Layout.TechBase != "IS" {
			su.TechBase = r.Layout.TechBase
		}
		for loc, slots := range r.Layout.Slots {
			row := make([]any, len(slots))
			for i, s := range slots {
				switch {
				case s.Mount > 0:
					row[i] = s.Mount
				case s.System != "":
					row[i] = s.System
				default:
					row[i] = 0
				}
			}
			su.Slots[loc] = row
		}
		su.Mounts = make([][]any, 0, len(r.Layout.Mounts))
		for _, m := range r.Layout.Mounts {
			su.Mounts = append(su.Mounts, []any{m.Equipment, m.Loc, m.Slots})
		}
		st.Designs[uuid] = su
	}
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if err := gzipTo(layouts, b); err != nil {
		return err
	}
	eqJSON, err := mtf.WriteEquipments(t.eq)
	if err != nil {
		return err
	}
	return gzipTo(equipment, eqJSON)
}

func gunzip(b []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

func gzipTo(w io.Writer, b []byte) error {
	zw, _ := gzip.NewWriterLevel(w, gzip.BestCompression)
	if _, err := zw.Write(b); err != nil {
		return err
	}
	return zw.Close()
}

// Parsed is one design file read for a table.
type Parsed struct {
	UUID, Hash string
	Source     []byte
}

// Build makes a table from design files (unit-file name and hash pairs from MekBay's units manifest, with their sources) read with eq (MekBay's equipment
// file with hash eqFile), on top of base
// (nil for none): a design whose file changed gets the new revision first and keeps its older ones. Files that fail to parse are logged and skipped.
func Build(base *Table, units, eqFile string, eq *mtf.Equipments, files []Parsed) *Table {
	t := &Table{Units: units, Equipment: digest(eq), EqFile: eqFile, eq: eq, byUUID: map[string][]Revision{}}
	if base != nil {
		for k, v := range base.byUUID {
			t.byUUID[k] = v
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].UUID < files[j].UUID })
	for _, f := range files {
		l, err := mtf.Parse(f.Source, eq)
		if err != nil {
			log.Printf("layouts: %s: %v", f.UUID, err)
			continue
		}
		rev := Revision{Hash: f.Hash, Canary: mtf.Canary(f.Source), Layout: l}
		old := t.byUUID[f.UUID]
		revs := []Revision{rev}
		for _, o := range old {
			if o.Canary != rev.Canary && len(revs) < maxRevisions {
				revs = append(revs, o)
			}
		}
		t.byUUID[f.UUID] = revs
	}
	return t
}

// maxRevisions bounds the revisions kept of one design.
const maxRevisions = 3

// digest sums what of the equipment catalog decides a layout, so a catalog that changed only elsewhere (prices, rules references) changes no layout.
func digest(eq *mtf.Equipments) string {
	b, _ := mtf.WriteEquipments(eq)
	return mtf.HashBytes(b)
}

// Empty is a table with no layouts.
func Empty() *Table {
	return &Table{eq: mtf.NewEquipments(nil), byUUID: map[string][]Revision{}}
}
