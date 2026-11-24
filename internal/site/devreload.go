package site

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Live reload for development (the -dev-reload flag, which .air.toml passes). Air's own browser-reload proxy
// can't be used: it rewrites the Host (so sameOrigin refuses every POST and the mekbay-<id> hosts never reach
// the MekBay proxy) and it drops WebSocket upgrades. Instead every page keeps an EventSource open to
// GET /dev/reload, which sends this process's boot ID and then holds the connection. When Air restarts the
// server the connection drops, the browser reconnects to the new process, sees a different boot ID and
// reloads (static/devreload.js). A reconnect to the same process (a network blip) reloads nothing.

// WithDevReload turns on the live-reload endpoint and the script that every page loads for it.
func WithDevReload() Option {
	return func(s *Server) { s.bootID = strconv.FormatInt(time.Now().UnixNano(), 36) }
}

func (s *Server) devReload(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	// retry: reconnect quickly once the restarted server is listening again.
	fmt.Fprintf(w, "retry: 250\ndata: %s\n\n", s.bootID)
	flusher.Flush()
	tick := time.NewTicker(30 * time.Second) // a comment line now and then, so nothing in between closes an idle stream
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
