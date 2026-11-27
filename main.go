// Command battletech serves the campaign folders as a website.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/davidzech/Battletech/internal/mekbayproxy"
	"github.com/davidzech/Battletech/internal/site"
	"github.com/davidzech/Battletech/internal/storage"
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
	devReload := flag.Bool("dev-reload", false, "reload open pages when the server restarts (for air; never in production)")
	flag.Parse()

	var (
		store *storage.Store
		err   error
		desc  string
	)
	if *pgURL != "" {
		store, err = storage.OpenPostgres(*pgURL)
		desc = "postgres"
	} else {
		store, err = storage.OpenSQLite(*dbPath)
		desc = "sqlite " + *dbPath
	}
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// Hangars MekBay still holds (from before they lived in the database) are imported before anything is served. A failure stops the server: on a host that
	// deploys a new version only once it starts, the old version keeps serving.
	if err := site.ImportHangars(context.Background(), store, nil); err != nil {
		store.Close()
		log.Fatalf("import the hangars from MekBay: %v", err)
	}

	go func() { // a MekBay release that changes what the proxy relies on shows here, not as a player's broken table link
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := mekbayproxy.CheckMekBay(ctx); err != nil {
			log.Printf("warning: the MekBay proxy may not work with the current mekbay.com: %v", err)
		} else {
			log.Printf("MekBay proxy check: ok")
		}
	}()

	log.Printf("serving %q on %s (database: %s)", *content, *addr, desc)
	var opts []site.Option
	if *devReload {
		opts = append(opts, site.WithDevReload())
		log.Printf("live reload on: open pages reload when the server restarts")
	}
	// Only the wait for a request's headers and an idle keep-alive connection are timed. A read or write timeout would cut the MekBay relay's
	// WebSocket, the live-reload stream and requests that wait on MekBay.
	srv := &http.Server{
		Addr:              *addr,
		Handler:           site.NewHandler(*content, store, opts...),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	stopped, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	failed := make(chan error, 1)
	go func() { failed <- srv.ListenAndServe() }()
	select {
	case err := <-failed:
		store.Close()
		log.Fatal(err)
	case <-stopped.Done():
	}
	stop() // a second signal stops the process at once
	// Requests in progress get a moment to finish, so a change to a campaign is not cut off half-way; then whatever is left (the streams that never
	// end by themselves) is closed. With live reload on, every open page holds such a stream, so there is nothing to wait for.
	grace := shutdownGrace
	if *devReload {
		grace = 0
	}
	log.Printf("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		srv.Close()
	}
}

// shutdownGrace is how long requests in progress may take to finish once the server is told to stop. It stays under the ten seconds a container
// runtime usually waits before it kills the process, so the database is still closed properly.
const shutdownGrace = 8 * time.Second
