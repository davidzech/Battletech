//go:build ignore

// gen_icons.go vendors the unit icons the site shows into internal/site/icons, so nothing is hotlinked from MekBay or
// GitHub. The icons come from MegaMek's mm-data repository (data/images/units), licensed CC BY-NC-SA 4.0; see
// icons/NOTICE.md. mm-data is large (hundreds of MB), so fetch only the two folders needed:
//
//	git clone --depth 1 --filter=blob:none --sparse https://github.com/MegaMek/mm-data.git
//	cd mm-data && git sparse-checkout set data/images/units/meks data/images/units/defaults
//
// then, from internal/site:
//
//	go run gen_icons.go gen_common.go -src <path to mm-data>/data/images/units
//
// It copies the icon of every mech in ../../data/units.json (and a default icon per weight class, for units without
// one) under the flat names gen_units.go records in units_index.json.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	src := flag.String("src", "", "mm-data's data/images/units folder")
	flag.Parse()
	if *src == "" {
		log.Fatal("give -src <mm-data>/data/images/units")
	}
	raw, err := os.ReadFile("../../data/units.json")
	if err != nil {
		log.Fatal(err)
	}
	var doc struct {
		Units []struct {
			Type string `json:"type"`
			Icon string `json:"icon"`
		} `json:"units"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		log.Fatal(err)
	}

	want := map[string]string{} // flat name -> source path inside -src
	for _, u := range doc.Units {
		if u.Type != "Mek" || u.Icon == "" {
			continue
		}
		name := iconFile(u.Icon)
		if prev, ok := want[name]; ok && prev != u.Icon {
			log.Fatalf("icon name clash: %q and %q both become %q", prev, u.Icon, name)
		}
		want[name] = u.Icon
	}
	for _, class := range []string{"light", "medium", "heavy", "assault", "tripod", "quad"} {
		want["default_"+class+".png"] = "defaults/default_" + class + ".png"
	}

	if err := os.MkdirAll("icons", 0o755); err != nil {
		log.Fatal(err)
	}
	old, _ := filepath.Glob("icons/*.png")
	for _, f := range old {
		if err := os.Remove(f); err != nil {
			log.Fatal(err)
		}
	}

	names := make([]string, 0, len(want))
	for n := range want {
		names = append(names, n)
	}
	sort.Strings(names)
	var copied, missing, bytes int
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(*src, filepath.FromSlash(want[n])))
		if err != nil {
			missing++
			fmt.Fprintf(os.Stderr, "missing %s (%s)\n", want[n], strings.TrimSpace(err.Error()))
			continue
		}
		if err := os.WriteFile(filepath.Join("icons", n), b, 0o644); err != nil {
			log.Fatal(err)
		}
		copied++
		bytes += len(b)
	}
	log.Printf("copied %d icons (%.1f MB) into icons/, %d missing (those fall back to a default icon)", copied, float64(bytes)/1e6, missing)
}
