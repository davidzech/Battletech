package site

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const ownerUID = "e66a3544-8fcf-4dab-955a-f4ea2db0b610"

func TestLookupUnitAndParseMul(t *testing.T) {
	for _, c := range []struct{ chassis, model, name string }{
		{"Koshi (Mist Lynx)", "E", "BMKoshi_E"},
		{"Grizzly", "", "BMGrizzly"},
		{"Mad Cat (Timber Wolf)", "H", "BMMadCat_H"},
	} {
		if u, ok := lookupUnit(c.chassis, c.model); !ok || u.Name != c.name || u.BV <= 0 {
			t.Errorf("lookupUnit(%q, %q) = %+v, %v; want %s", c.chassis, c.model, u, ok, c.name)
		}
	}
	if _, ok := lookupUnit("No Such Mech", "X"); ok {
		t.Error("unknown unit should not be found")
	}

	raw, err := os.ReadFile("../../campaigns/RefusalWar/Scenario_000/OpFor1.mul")
	if err != nil {
		t.Fatal(err)
	}
	es, err := parseMul(raw)
	if err != nil || len(es) != 3 {
		t.Fatalf("parseMul = %v, %v", es, err)
	}
	if es[0].Chassis != "Koshi (Mist Lynx)" || es[0].Model != "E" || es[0].Pilot != "Tyra" || es[0].Gunnery != 3 || es[0].Piloting != 4 {
		t.Errorf("first entity = %+v", es[0])
	}
	if _, err := parseMul([]byte("not xml")); err == nil {
		t.Error("garbage should not parse")
	}
}

func TestEveryScenarioForceResolves(t *testing.T) {
	lib := Library{Root: "../../campaigns"}
	cs, err := lib.Campaigns()
	if err != nil {
		t.Fatal(err)
	}
	for i := range cs {
		for j := range cs[i].Scenarios {
			sc := &cs[i].Scenarios[j]
			if len(sc.Forces) == 0 {
				continue
			}
			d, err := opForDraft(cs[i], sc)
			if err != nil {
				t.Errorf("%s/%s: %v", cs[i].Slug, sc.Slug, err)
				continue
			}
			if len(d.Groups) != len(sc.Forces) {
				t.Errorf("%s: %d groups for %d force files", sc.Slug, len(d.Groups), len(sc.Forces))
			}
		}
	}
}

func TestBuildForceIsPristine(t *testing.T) {
	d := ForceDraft{Name: "X - OpFor", Groups: []GroupDraft{{Name: "OpFor 1", Units: []UnitDraft{{Unit: "BMGrizzly", BV: 1500, Pilot: "Drusus", Gunnery: 3, Piloting: 4}}}, {Name: "OpFor 2", Units: []UnitDraft{{Unit: "BMLoki_Prime", BV: 2000}}}}}
	f := buildForce("force-id", time.Date(2026, 11, 16, 7, 4, 11, 0, time.UTC), d)
	b, _ := json.Marshal(f)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["instanceId"] != "force-id" || m["type"] != "cbt" || m["bv"] != float64(3500) || m["timestamp"] != "2026-11-16T07:04:11.000Z" {
		t.Errorf("force header = %s", b)
	}
	if len(f.Groups) != 2 || f.Groups[0].Name != "OpFor 1" || f.Groups[0].Units[0].Unit != "BMGrizzly" {
		t.Fatalf("groups = %+v", f.Groups)
	}
	st := f.Groups[0].Units[0].State
	if st.Modified || st.Destroyed || len(st.Locations) != 0 || st.Crew[0].Name != "Drusus" || st.Crew[0].Gunnery != 3 {
		t.Errorf("unit state = %+v", st)
	}
	if f.Groups[0].ID == f.Groups[1].ID || f.Groups[0].Units[0].ID == f.Groups[1].Units[0].ID {
		t.Error("groups and units need distinct IDs")
	}
}

// fakeMekBay records requests instead of calling mekbay.com.
type fakeMekBay struct {
	mu   sync.Mutex
	reqs []OperationRequest
	err  error
}

func (f *fakeMekBay) SyncOperation(_ context.Context, req OperationRequest) (OperationResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	if f.err != nil {
		return OperationResult{}, f.err
	}
	res := OperationResult{OpForInstance: req.OpForInstance, OperationID: req.OperationID}
	if res.OpForInstance == "" {
		res.OpForInstance = "opfor-" + goodInstance[:8]
	}
	if res.OperationID == "" {
		res.OperationID = "op-" + goodInstance[:8]
	}
	return res, nil
}

func newMekBayTestServer(t *testing.T, mb MekBay) (*httptest.Server, *Store) {
	t.Helper()
	store, err := OpenSQLite(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	srv := httptest.NewServer(NewHandler("../../campaigns", store, WithMekBay(mb)))
	t.Cleanup(srv.Close)
	return srv, store
}

func TestCreateOperationFlow(t *testing.T) {
	fake := &fakeMekBay{}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	const page = "/refusalwar/scenario_000/"
	form := func(extra ...string) url.Values {
		v := url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_000"}}
		for i := 0; i+1 < len(extra); i += 2 {
			v.Set(extra[i], extra[i+1])
		}
		return v
	}

	if !strings.Contains(body(t, srv, page, nil), "Trial of Position") {
		t.Fatal("test page path is wrong: the scenario page did not load")
	}

	// No seed: refused, and the page offers nothing.
	if r := post(t, srv, "/api/mekbay/operation", form(), nil, srv.URL); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("no seed: %d", r.StatusCode)
	}
	if strings.Contains(body(t, srv, page, nil), `id="mekbay-op"`) {
		t.Fatal("the MekBay section must be hidden without a registered hangar")
	}

	res := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "")
	cookie := res.Cookies()[0]
	id := playthroughID("wolfpack2026")

	// Seed but no hangar: refused, section hidden.
	if r := post(t, srv, "/api/mekbay/operation", form(), cookie, srv.URL); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("no hangar: %d", r.StatusCode)
	}
	if strings.Contains(body(t, srv, page, cookie), `id="mekbay-op"`) {
		t.Fatal("the MekBay section must be hidden before the hangar is registered")
	}

	// Hangar but no owner UID: read-only, so the section explains how to enable it and creating is refused.
	if r := post(t, srv, "/api/campaign", url.Values{"hangar": {goodHangar}, "players": {"2"}}, cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("setup: %d", r.StatusCode)
	}
	p := body(t, srv, page, cookie)
	if !strings.Contains(p, `id="mekbay-op"`) || strings.Contains(p, `id="mekbay-create"`) || !strings.Contains(p, "add your MekBay user ID") {
		t.Fatalf("read-only page should explain modify mode, got: %.600s", p)
	}
	if r := post(t, srv, "/api/mekbay/operation", form(), cookie, srv.URL); r.StatusCode != http.StatusForbidden {
		t.Fatalf("read-only create: %d", r.StatusCode)
	}

	// Modify mode: the page offers the button, and creating hands the owner UID and hangar to MekBay.
	if r := post(t, srv, "/api/campaign", url.Values{"hangar": {goodHangar}, "players": {"2"}, "owner": {ownerUID}}, cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("setup with owner: %d", r.StatusCode)
	}
	if p := body(t, srv, page, cookie); !strings.Contains(p, `id="mekbay-create"`) {
		t.Fatal("modify mode should offer the create button")
	}
	if r := post(t, srv, "/api/mekbay/operation", form(), cookie, "https://evil.example"); r.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin: %d", r.StatusCode)
	}
	if r := post(t, srv, "/api/mekbay/operation", form("scenario", "scenario_999"), cookie, srv.URL); r.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown scenario: %d", r.StatusCode)
	}
	r := post(t, srv, "/api/mekbay/operation", form(), cookie, srv.URL)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("create: %d", r.StatusCode)
	}
	var out map[string]string
	_ = json.NewDecoder(r.Body).Decode(&out)
	if out["url"] != "https://mekbay.com/?operation=op-"+goodInstance[:8] {
		t.Fatalf("url = %q", out["url"])
	}
	if len(fake.reqs) != 1 {
		t.Fatalf("expected one MekBay call, got %d", len(fake.reqs))
	}
	req := fake.reqs[0]
	if req.OwnerUID != ownerUID || req.CampaignInstance != goodInstance || req.OperationName != "Refusal War - Scenario 000" || req.OpForInstance != "" {
		t.Errorf("request = %+v", req)
	}
	if len(req.OpFor.Groups) != 2 || req.OpFor.Groups[0].Name != "OpFor 1" || req.OpFor.Groups[1].Units[1].Unit != "BMShadowCat_A" {
		t.Errorf("opfor groups = %+v", req.OpFor.Groups)
	}
	if op, ok, _ := store.MekBayOperation(ctx, id, "scenario_000"); !ok || op.OperationID == "" || op.OpForInstance == "" {
		t.Fatalf("operation not remembered: %+v", op)
	}

	// The page now links to the operation (and never shows the owner UID).
	p = body(t, srv, page, cookie)
	if !strings.Contains(p, `href="https://mekbay.com/?operation=op-`+goodInstance[:8]+`"`) || !strings.Contains(p, `id="mekbay-rebuild"`) || strings.Contains(p, ownerUID) {
		t.Fatalf("page after create: %.800s", p)
	}

	// Posting again without rebuild does not call MekBay; with rebuild it reuses the same force and operation.
	post(t, srv, "/api/mekbay/operation", form(), cookie, srv.URL)
	if len(fake.reqs) != 1 {
		t.Fatalf("repeat without rebuild called MekBay: %d calls", len(fake.reqs))
	}
	post(t, srv, "/api/mekbay/operation", form("rebuild", "1"), cookie, srv.URL)
	if len(fake.reqs) != 2 || fake.reqs[1].OpForInstance != "opfor-"+goodInstance[:8] || fake.reqs[1].OperationID != "op-"+goodInstance[:8] {
		t.Fatalf("rebuild should reuse the stored IDs: %+v", fake.reqs)
	}

	// MekBay errors reach the player, without storing anything new.
	fake.err = errors.New("MekBay says that user ID doesn't own this hangar, so it can't be changed")
	if r := post(t, srv, "/api/mekbay/operation", form("rebuild", "1"), cookie, srv.URL); r.StatusCode != http.StatusBadGateway {
		t.Fatalf("mekbay error: %d", r.StatusCode)
	}
}

// TestMekBayClientProtocol runs the real client against a fake MekBay WebSocket server and checks the
// sequence of messages: register, getForce, saveForce, saveOperation, getOperation.
func TestMekBayClientProtocol(t *testing.T) {
	var (
		mu      sync.Mutex
		actions []string
		saved   map[string]any
		op      map[string]any
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		reply := func(rid string, v map[string]any) {
			v["requestId"] = rid
			b, _ := json.Marshal(v)
			_ = c.Write(ctx, websocket.MessageText, b)
		}
		for {
			_, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			rid, _ := m["requestId"].(string)
			mu.Lock()
			actions = append(actions, m["action"].(string))
			mu.Unlock()
			if m["sessionId"] == nil || rid == "" {
				t.Errorf("message without session or request id: %s", b)
			}
			switch m["action"] {
			case "register":
				if m["uuid"] != ownerUID || m["version"] != float64(2) {
					t.Errorf("register = %s", b)
				}
				reply("", map[string]any{"action": "userState"}) // unrelated pushes must be skipped
			case "getForce":
				reply(rid, map[string]any{"action": "forceData", "data": map[string]any{"timestamp": "2026-11-16T07:04:11.406Z"}})
			case "saveForce":
				mu.Lock()
				saved = m["data"].(map[string]any)
				mu.Unlock()
				reply(rid, map[string]any{"action": "forceSaved"})
			case "saveOperation": // no reply, like the real server
				mu.Lock()
				op = m["data"].(map[string]any)
				mu.Unlock()
			case "getOperation":
				mu.Lock()
				data := op
				mu.Unlock()
				reply(rid, map[string]any{"action": "operationData", "data": data})
			}
		}
	}))
	defer ts.Close()

	cl := &MekBayClient{URL: "ws" + strings.TrimPrefix(ts.URL, "http")}
	d := ForceDraft{Name: "N - OpFor", Groups: []GroupDraft{{Name: "OpFor 1", Units: []UnitDraft{{Unit: "BMGrizzly", BV: 5}}}}}
	res, err := cl.SyncOperation(context.Background(), OperationRequest{OwnerUID: ownerUID, CampaignInstance: goodInstance, OpFor: d, OperationName: "N"})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := strings.Join(actions, ","); got != "register,getForce,saveForce,saveOperation,getOperation" {
		t.Fatalf("actions = %s", got)
	}
	if saved["instanceId"] != res.OpForInstance || saved["name"] != "N - OpFor" {
		t.Errorf("saved force = %v", saved)
	}
	forces := op["forces"].([]any)
	f0, f1 := forces[0].(map[string]any), forces[1].(map[string]any)
	if op["operationId"] != res.OperationID || f0["instanceId"] != goodInstance || f0["alignment"] != "friendly" ||
		f0["timestamp"] != "2026-11-16T07:04:11.406Z" || f1["instanceId"] != res.OpForInstance || f1["alignment"] != "enemy" {
		t.Errorf("operation = %v", op)
	}
}

func TestMekBayClientReportsRefusals(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := websocket.Accept(w, r, nil)
		defer c.CloseNow()
		for {
			_, b, err := c.Read(r.Context())
			if err != nil {
				return
			}
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			rid, _ := m["requestId"].(string)
			var out map[string]any
			switch m["action"] {
			case "getForce":
				out = map[string]any{"action": "forceData", "requestId": rid, "data": map[string]any{"timestamp": "t"}}
			case "saveForce":
				out = map[string]any{"action": "warning", "requestId": rid, "code": "not_owner", "message": "You do not own this force"}
			default:
				continue
			}
			bb, _ := json.Marshal(out)
			_ = c.Write(r.Context(), websocket.MessageText, bb)
		}
	}))
	defer ts.Close()
	cl := &MekBayClient{URL: "ws" + strings.TrimPrefix(ts.URL, "http")}
	_, err := cl.SyncOperation(context.Background(), OperationRequest{OwnerUID: ownerUID, CampaignInstance: goodInstance, OperationName: "N"})
	if err == nil || !strings.Contains(err.Error(), "doesn't own") {
		t.Fatalf("expected a not-owner error, got %v", err)
	}
}

// TestMekBayLive talks to the real mekbay.com and creates a force and an operation in the given account. It only
// runs when MEKBAY_LIVE_UID and MEKBAY_LIVE_INSTANCE (the account's UID and one of its saved forces) are set.
func TestMekBayLive(t *testing.T) {
	uid, instance := os.Getenv("MEKBAY_LIVE_UID"), os.Getenv("MEKBAY_LIVE_INSTANCE")
	if uid == "" || instance == "" {
		t.Skip("set MEKBAY_LIVE_UID and MEKBAY_LIVE_INSTANCE to run against mekbay.com")
	}
	lib := Library{Root: "../../campaigns"}
	c, _ := lib.Campaign("refusalwar")
	sc, _ := c.Scenario("scenario_000")
	draft, err := opForDraft(c, sc)
	if err != nil {
		t.Fatal(err)
	}
	res, err := (&MekBayClient{}).SyncOperation(context.Background(), OperationRequest{
		OwnerUID: uid, CampaignInstance: instance, OpFor: draft, OperationName: operationName(c, sc) + " (live test)",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("operation %s, opfor force %s", OperationURL(res.OperationID), res.OpForInstance)
}
