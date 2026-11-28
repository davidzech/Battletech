package layouts

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/davidzech/Battletech/internal/mtf"
)

// Where MekBay's web app publishes its unit files (core-unit-manifest.ts, repository-asset-manifest.service.ts, equipment-catalog.service.ts).
const (
	assetsPath    = "/online-assets/assets-manifest.json"
	manifestPath  = "/online-assets/generated/units-manifest.json"
	archivePath   = "/online-assets/generated/units.zip"
	unitDir       = "/online-assets/generated/units/"
	equipmentPath = "/online-assets/static/equipment.json"
	// fetchOneByOne is how many changed files are fetched one by one rather than as the whole archive (MAX_INCREMENTAL_UNIT_REQUESTS).
	fetchOneByOne = 30
	// MekBay's sites: its release, and its next release until that becomes the release.
	Release = "https://mekbay.com"
	Next    = "https://next.mekbay.com"
)

// ErrNoUnitFiles is the answer of a MekBay site that doesn't publish its unit files: one still running the release before version 2 (mekbay.com before
// next.mekbay.com becomes it).
var ErrNoUnitFiles = errors.New("this MekBay doesn't publish unit files")

// client is the HTTP client syncs use; tests replace it.
var client = &http.Client{Timeout: 2 * time.Minute}

// Assets reads a MekBay site's assets manifest: the hashes of its unit manifest, unit archive and equipment catalog. ErrNoUnitFiles when it has none.
func Assets(ctx context.Context, site string) (units, archive, equipment string, err error) {
	b, err := get(ctx, site+assetsPath, 4<<20)
	if err != nil {
		return "", "", "", err
	}
	var m map[string]string
	if json.Unmarshal(b, &m) != nil || m[strings.TrimPrefix(manifestPath, "/")] == "" {
		return "", "", "", ErrNoUnitFiles // the web app's own page, served for any path it doesn't know
	}
	return m[strings.TrimPrefix(manifestPath, "/")], m[strings.TrimPrefix(archivePath, "/")], m[strings.TrimPrefix(equipmentPath, "/")], nil
}

// Sync brings the table in use up to date with a MekBay site's unit files. It fetches the small assets manifest, and when that names other files than the
// table was built from, the unit manifest and only the unit files whose hash changed (the whole archive when there are many, or when the equipment
// catalog changed in a way that may move components). It never makes the table worse: on any failure the table stays as it was.
func Sync(ctx context.Context, site string) error {
	cur := Current()
	next, n, err := Fetch(ctx, site, cur)
	if err != nil || next == nil {
		return err
	}
	if current.CompareAndSwap(cur, next) {
		log.Printf("layouts: %d design files updated from %s (%d designs)", n, site, next.Len())
	}
	return nil
}

// Fetch builds a table from a MekBay site's unit files on top of cur (an empty table for a fresh one, as the generator builds), fetching only what cur
// doesn't already hold; it also answers how many design files it read. A nil table means cur is up to date.
func Fetch(ctx context.Context, site string, cur *Table) (*Table, int, error) {
	units, _, eqHash, err := Assets(ctx, site)
	if err != nil {
		return nil, 0, err
	}
	if units == cur.Units {
		return nil, 0, nil
	}
	eq := cur.eq
	if eqHash != cur.EqFile {
		b, err := get(ctx, site+equipmentPath, 64<<20)
		if err != nil {
			return nil, 0, err
		}
		fresh, err := mtf.ReadEquipments(bytes.NewReader(b))
		if err != nil {
			return nil, 0, err
		}
		eq = fresh
	}
	b, err := get(ctx, site+manifestPath, 16<<20)
	if err != nil {
		return nil, 0, err
	}
	var manifest map[string]string
	if err := json.Unmarshal(b, &manifest); err != nil {
		return nil, 0, fmt.Errorf("MekBay's unit manifest is unreadable: %w", err)
	}
	reparse := digest(eq) != cur.Equipment // the equipment changed: every layout is read again
	want := map[string]string{}            // file → hash, for the files to fetch
	for file, hash := range manifest {
		uuid, ok := strings.CutSuffix(file, ".mtf")
		if !ok {
			continue // vehicles and the rest: the site lays out mechs only
		}
		if rev, ok := cur.Newest(uuid); reparse || !ok || rev.Hash != hash {
			want[file] = hash
		}
	}
	var files []Parsed
	if len(want) > fetchOneByOne {
		zb, err := get(ctx, site+archivePath, 512<<20)
		if err != nil {
			return nil, 0, err
		}
		zr, err := zip.NewReader(bytes.NewReader(zb), int64(len(zb)))
		if err != nil {
			return nil, 0, fmt.Errorf("MekBay's unit archive is unreadable: %w", err)
		}
		for _, zf := range zr.File {
			hash, ok := want[zf.Name]
			if !ok {
				continue
			}
			rc, err := zf.Open()
			if err != nil {
				return nil, 0, err
			}
			src, err := io.ReadAll(io.LimitReader(rc, 8<<20))
			rc.Close()
			if err != nil {
				return nil, 0, err
			}
			files = append(files, Parsed{UUID: strings.TrimSuffix(zf.Name, ".mtf"), Hash: hash, Source: src})
		}
	} else {
		for file, hash := range want {
			src, err := get(ctx, site+unitDir+file, 8<<20)
			if err != nil {
				return nil, 0, err
			}
			files = append(files, Parsed{UUID: strings.TrimSuffix(file, ".mtf"), Hash: hash, Source: src})
		}
	}
	for _, f := range files {
		if mtf.HashBytes(f.Source) != f.Hash {
			return nil, 0, fmt.Errorf("MekBay's unit file %s doesn't match its manifest", f.UUID)
		}
	}
	return Build(cur, units, eqHash, eq, files), len(files), nil
}

// Keep syncs at once and then once a day, until ctx ends, from MekBay's release, or from its next release while the release doesn't publish unit files
// (or always, with next). A failed sync is logged and tried again an hour later.
func Keep(ctx context.Context, next bool) {
	for {
		wait := 24 * time.Hour
		if err := syncBest(ctx, next); err != nil {
			log.Printf("layouts: %v", err)
			wait = time.Hour
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func syncBest(ctx context.Context, next bool) error {
	if !next {
		err := Sync(ctx, Release)
		if !errors.Is(err, ErrNoUnitFiles) {
			return err
		}
	}
	return Sync(ctx, Next)
}

var getMu sync.Mutex // one download at a time: syncs are rare, and MekBay is someone else's server

func get(ctx context.Context, url string, limit int64) ([]byte, error) {
	getMu.Lock()
	defer getMu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach MekBay: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MekBay answered %s for %s", resp.Status, url)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", url, err)
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s is larger than expected", url)
	}
	return b, nil
}
