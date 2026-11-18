// Command battletech serves the campaign folders as a website.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/davidzech/Battletech/internal/site"
)

// defaultAddr listens on every interface, except under `go run`, where it stays on localhost so
// Windows Firewall doesn't ask to allow each freshly built temporary binary.
func defaultAddr() string {
	if exe, err := os.Executable(); err == nil && strings.Contains(exe, "go-build") {
		return "localhost:8080"
	}
	return ":8080"
}

func main() {
	addr := flag.String("addr", defaultAddr(), "address to listen on; under go run the default is localhost only")
	content := flag.String("content", "campaigns", "folder holding the campaign folders")
	dbPath := flag.String("db", "playthroughs.db", "SQLite database file for playthrough state (used when no PostgreSQL URL is set)")
	pgURL := flag.String("postgres", os.Getenv("DATABASE_URL"), "PostgreSQL connection URL; defaults to $DATABASE_URL, and when set it is used instead of -db")
	mekbayKey := flag.String("mekbay-link-key", os.Getenv("MEKBAY_LINK_KEY"), "secret that MekBay links seal their accounts under; defaults to $MEKBAY_LINK_KEY, and without one links stop working when the process restarts")
	flag.Parse()

	var (
		store *site.Store
		err   error
		desc  string
	)
	if *pgURL != "" {
		store, err = site.OpenPostgres(*pgURL)
		desc = "postgres"
	} else {
		store, err = site.OpenSQLite(*dbPath)
		desc = "sqlite " + *dbPath
	}
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	if *mekbayKey == "" {
		log.Printf("warning: no MEKBAY_LINK_KEY is set, so MekBay links stop working whenever the server restarts")
	}
	go func() { // a MekBay release that changes what the proxy relies on shows here, not as a player's broken table link
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := site.CheckMekBay(ctx); err != nil {
			log.Printf("warning: the MekBay proxy may not work with the current mekbay.com: %v", err)
		} else {
			log.Printf("MekBay proxy check: ok")
		}
	}()

	log.Printf("serving %q on %s (database: %s)", *content, *addr, desc)
	log.Fatal(http.ListenAndServe(*addr, site.NewHandler(*content, store, site.WithMekBayLinkKey(*mekbayKey))))
}
