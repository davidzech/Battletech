//go:build ignore

// gen_layouts builds the embedded layout table (layouts.json.gz and equipment.json.gz) from MekBay's published unit files: go generate
// ./internal/layouts. It downloads MekBay's whole unit archive (about 40 MB), from its release when that publishes unit files, else from its next
// release; -site picks one. The server keeps the table up to date by itself (layouts.Keep), so regenerating is only to shorten what it fetches.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"time"

	"github.com/davidzech/Battletech/internal/layouts"
)

func main() {
	site := flag.String("site", "", "MekBay site to read (default: "+layouts.Release+", else "+layouts.Next+")")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	sites := []string{*site}
	if *site == "" {
		sites = []string{layouts.Release, layouts.Next}
	}
	var t *layouts.Table
	var n int
	var err error
	for _, s := range sites {
		t, n, err = layouts.Fetch(ctx, s, layouts.Empty())
		if !errors.Is(err, layouts.ErrNoUnitFiles) {
			log.Printf("read %d design files from %s", n, s)
			break
		}
	}
	if err != nil {
		log.Fatal(err)
	}
	lf, err := os.Create("layouts.json.gz")
	if err != nil {
		log.Fatal(err)
	}
	ef, err := os.Create("equipment.json.gz")
	if err != nil {
		log.Fatal(err)
	}
	if err := t.Write(lf, ef); err != nil {
		log.Fatal(err)
	}
	if err := lf.Close(); err != nil {
		log.Fatal(err)
	}
	if err := ef.Close(); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote layouts.json.gz: %d designs", t.Len())
}
