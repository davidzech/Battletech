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
	flag.Parse()

	log.Printf("serving %q on %s", *content, *addr)
	log.Fatal(http.ListenAndServe(*addr, site.NewHandler(*content)))
}
