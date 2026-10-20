// Command battletech serves the campaign folders as a website.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strings"

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

	log.Printf("serving %q on %s (database: %s)", *content, *addr, desc)
	log.Fatal(http.ListenAndServe(*addr, site.NewHandler(*content, store)))
}
