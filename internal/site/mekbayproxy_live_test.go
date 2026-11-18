package site

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestMekBayProxyLive runs the proxy against the real mekbay.com with throwaway accounts (random UUIDs, one tiny force). It only runs when
// MEKBAY_LIVE_PROXY is set. It checks the things the design stands on: the WebSocket string is still there to replace, and a browser that
// believes it is a random visitor but carries the owner's link token can read, change and own the owner's force, while a browser with
// someone else's token can't touch it.
func TestMekBayProxyLive(t *testing.T) {
	if os.Getenv("MEKBAY_LIVE_PROXY") == "" {
		t.Skip("set MEKBAY_LIVE_PROXY=1 to run the proxy against mekbay.com")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store, err := OpenSQLite(t.TempDir() + "/live.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var proxy *mekbayProxy
	site := httptest.NewServer(NewHandler(t.TempDir(), store, WithMekBayTokenKey("live-test"), withMekBayProxyHook(func(p *mekbayProxy) { proxy = p })))
	defer site.Close()
	addr := strings.TrimPrefix(site.URL, "http://")
	_, port, _ := net.SplitHostPort(addr)
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	origin := "http://" + testHost + ":" + port

	// 1. The real MekBay still names its WebSocket the way the proxy expects.
	if err := proxy.Check(ctx); err != nil {
		t.Fatalf("MekBay's build no longer matches what the proxy rewrites: %v", err)
	}

	// 2. The real page is served, and the script that opens the socket points at us.
	get := func(path string) string {
		req, _ := http.NewRequestWithContext(ctx, "GET", origin+path, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var sb strings.Builder
		buf := make([]byte, 1<<16)
		for {
			n, err := resp.Body.Read(buf)
			sb.Write(buf[:n])
			if err != nil {
				break
			}
		}
		if resp.StatusCode != 200 {
			t.Fatalf("GET %s: %d", path, resp.StatusCode)
		}
		return sb.String()
	}
	if page := get("/"); !strings.Contains(page, "<app-root") && !strings.Contains(page, "main-") {
		t.Fatalf("the MekBay page was not served: %.200s", page)
	}

	// 3. A throwaway owner saves a tiny force, directly.
	owner, stranger, browserID := newID(), newID(), newID() // browserID is the random identity the page itself believes it has
	instance := newID()
	own, err := dialMekBay(ctx, mekbayWSURL, owner)
	if err != nil {
		t.Fatal(err)
	}
	defer own.close()
	force := buildForce(instance, time.Now().UTC(), ForceDraft{Name: "Proxy live test", Groups: []GroupDraft{{Name: "Test", Units: []UnitDraft{{Unit: "BMBlackHawk_Prime"}}}}})
	if _, err := own.call(ctx, map[string]any{"action": "saveForce", "uuid": owner, "data": force, "savedForceCount": 1}); err != nil {
		t.Fatalf("could not create the test force: %v", err)
	}

	// browser opens the proxy's relay with a link token for account, and registers as the random identity the page generated.
	browser := func(account string) *mekbaySession {
		t.Helper()
		tok, err := MekBayLinkToken("live-test", account, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		conn, _, err := websocket.Dial(ctx, "ws://"+testHost+":"+port+"/ws", &websocket.DialOptions{
			HTTPClient: client,
			HTTPHeader: http.Header{"Cookie": {mekbayCookie + "=" + tok}, "Origin": {origin}},
		})
		if err != nil {
			t.Fatalf("could not open the relay: %v", err)
		}
		conn.SetReadLimit(16 << 20)
		s := &mekbaySession{conn: conn, id: newID(), uuid: browserID}
		if err := s.send(ctx, map[string]any{"action": "register", "uuid": browserID, "version": mekbayProtocol}); err != nil {
			t.Fatal(err)
		}
		return s
	}

	// 4. The owner's browser, though it presents itself as someone else, is treated as the owner: ownedOnly only answers the owner.
	ob := browser(owner)
	defer ob.close()
	reply, err := ob.call(ctx, map[string]any{"action": "getForce", "uuid": browserID, "instanceId": instance, "ownedOnly": true})
	if err != nil {
		t.Fatalf("the owner's browser could not read its force: %v", err)
	}
	var got struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(reply, &got); err != nil || got.Data == nil || got.Data["instanceId"] != instance {
		t.Fatalf("the owner's browser should see its own force through the relay: %s", reply)
	}
	t.Logf("owned read OK: %v (owned=%v)", got.Data["name"], got.Data["owned"])

	// 5. ...and may change it.
	got.Data["name"] = "Proxy live test (edited through the relay)"
	got.Data["timestamp"] = mekbayTime(time.Now())
	delete(got.Data, "owned")
	if _, err := ob.call(ctx, map[string]any{"action": "saveForce", "uuid": browserID, "data": got.Data, "savedForceCount": 1}); err != nil {
		t.Fatalf("the owner's browser could not save its force: %v", err)
	}
	read, err := (&MekBayClient{}).ReadHangar(ctx, instance)
	if err != nil || read.Name != "Proxy live test (edited through the relay)" {
		t.Fatalf("the edit reached MekBay: %q %v", read.Name, err)
	}

	// 6. A browser carrying someone else's token can neither read it as its own nor change it.
	sb := browser(stranger)
	defer sb.close()
	if reply, err := sb.call(ctx, map[string]any{"action": "getForce", "uuid": browserID, "instanceId": instance, "ownedOnly": true}); err == nil {
		var g struct {
			Data map[string]any `json:"data"`
		}
		if json.Unmarshal(reply, &g) == nil && g.Data != nil {
			t.Errorf("a stranger's browser must not get the force as its own: %s", reply)
		}
	}
	got.Data["name"] = "hijacked"
	if _, err := sb.call(ctx, map[string]any{"action": "saveForce", "uuid": browserID, "data": got.Data, "savedForceCount": 1}); err == nil || !strings.Contains(err.Error(), "doesn't own") {
		t.Errorf("a stranger's save is refused as not_owner: %v", err)
	}
	if read, _ := (&MekBayClient{}).ReadHangar(ctx, instance); read.Name == "hijacked" {
		t.Error("the stranger's edit must not have landed")
	}
}
