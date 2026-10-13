// Command battletech serves the campaign folders as a website.
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/davidzech/Battletech/internal/site"
)

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	content := flag.String("content", "campaigns", "folder holding the campaign folders")
	dbPath := flag.String("db", "playthroughs.db", "SQLite database file for playthrough state")
	flag.Parse()

	store, err := site.OpenStore(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	log.Printf("serving %q on %s (database %q)", *content, *addr, *dbPath)
	log.Fatal(http.ListenAndServe(*addr, site.NewHandler(*content, store)))
}
