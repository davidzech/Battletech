package site

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/mekbayproxy"
	"github.com/davidzech/Battletech/internal/storage"
)

// TestMekBayProxyLive runs the proxy against the real mekbay.com with throwaway accounts (random UUIDs, one tiny force). It only runs when
// MEKBAY_LIVE_PROXY is set. It checks the things the design stands on: the WebSocket string is still there to replace, and a browser that
// believes it is a random visitor but is reached through the owner's link can read, change and own the owner's force, while a browser on
// someone else's link can't touch it.
func TestMekBayProxyLive(t *testing.T) {
	if os.Getenv("MEKBAY_LIVE_PROXY") == "" {
		t.Skip("set MEKBAY_LIVE_PROXY=1 to run the proxy against mekbay.com")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store, err := storage.OpenSQLite(t.TempDir() + "/live.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.EnsurePlaythrough(ctx, "pt"); err != nil {
		t.Fatal(err)
	}
	var proxy *mekbayproxy.Proxy
	site := httptest.NewServer(NewHandler(t.TempDir(), store, withMekBayProxyHook(func(p *mekbayproxy.Proxy) { proxy = p })))
	defer site.Close()
	addr := strings.TrimPrefix(site.URL, "http://")
	_, port, _ := net.SplitHostPort(addr)
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	owner, stranger, browserID := mekbay.NewID(), mekbay.NewID(), mekbay.NewID() // browserID is the random identity the page itself believes it has
	// linkHost is the host name of the link for an account, as the site would hand it out.
	linkHost := func(label, account string) string {
		t.Helper()
		id, err := store.EnsureMekBayLink(ctx, "pt", "camp", label, account, time.Hour, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return storage.LinkPrefix + id + ".localhost"
	}
	ownerHost, strangerHost := linkHost("owner", owner), linkHost("stranger", stranger)

	// 1. The real MekBay still names its WebSocket the way the proxy expects.
	if err := proxy.Check(ctx); err != nil {
		t.Fatalf("MekBay's build no longer matches what the proxy rewrites: %v", err)
	}

	// 2. The real page is served on the owner's link, and the script that opens the socket points at that host.
	get := func(host, path string) string {
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+host+":"+port+path, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			t.Fatalf("GET %s%s: %d", host, path, resp.StatusCode)
		}
		return string(b)
	}
	if page := get(ownerHost, "/"); !strings.Contains(page, "<app-root") && !strings.Contains(page, "main-") {
		t.Fatalf("the MekBay page was not served: %.200s", page)
	}

	// 3. A throwaway owner saves a tiny force, directly.
	instance := mekbay.NewID()
	own, err := mekbay.Dial(ctx, mekbayproxy.UpstreamWS, owner)
	if err != nil {
		t.Fatal(err)
	}
	defer own.Close()
	force := mekbay.BuildForce(instance, time.Now().UTC(), mekbay.ForceDraft{Name: "Proxy live test", Groups: []mekbay.GroupDraft{{Name: "Test", Units: []mekbay.UnitDraft{{Unit: "BMBlackHawk_Prime"}}}}})
	if _, err := own.Call(ctx, map[string]any{"action": "saveForce", "uuid": owner, "data": force, "savedForceCount": 1}); err != nil {
		t.Fatalf("could not create the test force: %v", err)
	}

	// browser opens the relay on a link's host and registers as the random identity the page generated.
	browser := func(host string) *mekbay.Session {
		t.Helper()
		conn, _, err := websocket.Dial(ctx, "ws://"+host+":"+port+"/ws", &websocket.DialOptions{
			HTTPClient: client,
			HTTPHeader: http.Header{"Origin": {"http://" + host + ":" + port}},
		})
		if err != nil {
			t.Fatalf("could not open the relay: %v", err)
		}
		conn.SetReadLimit(16 << 20)
		s := mekbay.NewSession(conn, browserID)
		if err := s.Send(ctx, map[string]any{"action": "register", "uuid": browserID, "version": mekbay.Protocol}); err != nil {
			t.Fatal(err)
		}
		return s
	}

	// 4. The owner's browser, though it presents itself as someone else, is treated as the owner: ownedOnly only answers the owner.
	ob := browser(ownerHost)
	defer ob.Close()
	reply, err := ob.Call(ctx, map[string]any{"action": "getForce", "uuid": browserID, "instanceId": instance, "ownedOnly": true})
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
	got.Data["timestamp"] = mekbay.FormatTime(time.Now())
	delete(got.Data, "owned")
	if _, err := ob.Call(ctx, map[string]any{"action": "saveForce", "uuid": browserID, "data": got.Data, "savedForceCount": 1}); err != nil {
		t.Fatalf("the owner's browser could not save its force: %v", err)
	}
	read, err := (&mekbay.Client{}).ReadHangar(ctx, instance)
	if err != nil || read.Name != "Proxy live test (edited through the relay)" {
		t.Fatalf("the edit reached MekBay: %q %v", read.Name, err)
	}

	// 6. A browser on someone else's link can neither read it as its own nor change it.
	sb := browser(strangerHost)
	defer sb.Close()
	if reply, err := sb.Call(ctx, map[string]any{"action": "getForce", "uuid": browserID, "instanceId": instance, "ownedOnly": true}); err == nil {
		var g struct {
			Data map[string]any `json:"data"`
		}
		if json.Unmarshal(reply, &g) == nil && g.Data != nil {
			t.Errorf("a stranger's browser must not get the force as its own: %s", reply)
		}
	}
	got.Data["name"] = "hijacked"
	if _, err := sb.Call(ctx, map[string]any{"action": "saveForce", "uuid": browserID, "data": got.Data, "savedForceCount": 1}); err == nil || !strings.Contains(err.Error(), "doesn't own") {
		t.Errorf("a stranger's save is refused as not_owner: %v", err)
	}
	if read, _ := (&mekbay.Client{}).ReadHangar(ctx, instance); read.Name == "hijacked" {
		t.Error("the stranger's edit must not have landed")
	}
}
