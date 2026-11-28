package mekbayproxy

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestCheckMekBayLive runs the startup check against the real mekbay.com: the proxy still finds the WebSocket address it rewrites. It runs only with
// MEKBAY_LIVE_PROXY=1.
func TestCheckMekBayLive(t *testing.T) {
	if os.Getenv("MEKBAY_LIVE_PROXY") == "" {
		t.Skip("set MEKBAY_LIVE_PROXY=1 to check against mekbay.com")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := CheckMekBay(ctx, ""); err != nil {
		t.Fatal(err)
	}
}
