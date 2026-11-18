package site

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestCheckpointsAndRollback(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	const seed = "wolfpack2026"
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {seed}}, nil, "").Cookies()[0]
	post(t, srv, "/api/account", url.Values{"account": {"Wolf Pack"}}, cookie, srv.URL)
	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	id := playthroughID(seed)
	form := func(kv ...string) url.Values {
		v := url.Values{"campaign": {"refusalwar"}}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Set(kv[i], kv[i+1])
		}
		return v
	}
	state := func() (run CampaignRun, sp map[string]int, fallen map[string]bool) {
		run, _, _ = store.Run(ctx, id, "refusalwar")
		sp, fallen = map[string]int{}, map[string]bool{}
		ps, _ := store.Pilots(ctx, id, "refusalwar")
		for _, p := range ps {
			sp[p.Name], fallen[p.Name] = p.TotalSP, p.Fallen
		}
		return
	}
	checkpoints := func() []Checkpoint {
		cps, err := store.Checkpoints(ctx, id, "refusalwar")
		if err != nil {
			t.Fatal(err)
		}
		return cps
	}
	pilotID := map[string]string{}
	ps, _ := store.Pilots(ctx, id, "refusalwar")
	for _, p := range ps {
		pilotID[p.Name] = p.ID
	}

	// Starting the campaign is the first checkpoint, with a frozen copy of the hangar.
	cps := checkpoints()
	if len(cps) != 1 || cps[0].Label != "Campaign started" || cps[0].HangarCopy == "" {
		t.Fatalf("the starting checkpoint: %+v", cps)
	}
	start0, sp0, _ := state()
	live := start0.HangarInstance
	if len(fake.copies) != 1 || fake.copies[0].Source != live || fake.copies[0].Target != cps[0].HangarCopy {
		t.Fatalf("the hangar was copied into the checkpoint: %+v", fake.copies)
	}

	// The Manage page's Danger zone has a Rollback button and a dialog listing the checkpoints.
	if p := body(t, srv, "/refusalwar/manage/", cookie); !strings.Contains(p, `id="rollback-open">Rollback</button>`) || !strings.Contains(p, `id="rollback-dialog"`) || !strings.Contains(p, "Campaign started") || !strings.Contains(p, `data-checkpoint="`+strconv.Itoa(cps[0].Seq)+`"`) {
		t.Error("the Manage page offers Rollback with the saved points listed")
	}

	// Change things: the warchest, a pilot's SP, a pilot killed in action. None of it makes a checkpoint.
	post(t, srv, "/api/warchest", form("funds", "777"), cookie, srv.URL)
	post(t, srv, "/api/pilots/earn", form("pilot", pilotID["Alrek"], "sp", "90"), cookie, srv.URL)
	post(t, srv, "/api/pilots/fallen", form("pilot", pilotID["Katarina"]), cookie, srv.URL)
	if run, sp, fallen := state(); run.Warchest != 777 || sp["Alrek"] != sp0["Alrek"]+90 || !fallen["Katarina"] || len(checkpoints()) != 1 {
		t.Fatalf("the changes took: %+v %v %v", run, sp, fallen)
	}

	// Rolling back to the starting checkpoint puts the warchest, the pilots' SP and who is alive back, and the frozen hangar over the live one.
	if got := post(t, srv, "/api/campaign/rollback", form("checkpoint", strconv.Itoa(cps[0].Seq)), cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("rollback: %d", got)
	}
	if run, sp, fallen := state(); run.Warchest != start0.Warchest || sp["Alrek"] != sp0["Alrek"] || fallen["Katarina"] || run.HangarInstance != live {
		t.Errorf("rolled back: %+v %v %v", run, sp, fallen)
	}
	last := fake.copies[len(fake.copies)-1]
	if last.Source != cps[0].HangarCopy || last.Target != live {
		t.Errorf("the frozen hangar was copied back over the live one: %+v", last)
	}

	// Completing a mission is a checkpoint: the state after it, with the mission completed and its rewards in.
	if got := post(t, srv, "/api/campaign/scenario-outcome/commit", url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_000"}, "inputs": {`{"pilots":[],"mission":{}}`}}, cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("complete 000: %d", got)
	}
	cps = checkpoints()
	if len(cps) != 2 || cps[0].Label != "Completed Sortie 000: Trial of Position" || cps[0].HangarCopy == "" || cps[0].HangarCopy == cps[1].HangarCopy {
		t.Fatalf("the mission's checkpoint: %+v", cps)
	}
	after000, _, _ := state()

	// Between missions, things change (a pilot's SP, the funds), and a battle starts for the next mission.
	post(t, srv, "/api/pilots/earn", form("pilot", pilotID["Alrek"], "sp", "40"), cookie, srv.URL)
	post(t, srv, "/api/warchest", form("funds", "1"), cookie, srv.URL)
	if err := store.SaveMekBayOperation(ctx, id, "refusalwar", "scenario_001", MekBayOperation{OpForInstance: "opfor", OperationID: "op-1"}); err != nil {
		t.Fatal(err)
	}
	_, spBefore, _ := state()
	// Aborting the mission rolls back to the end of the previous one: the funds and SP are back, the battle is gone, the mission-000 completion stays.
	if got := post(t, srv, "/api/battle/abort", form("scenario", "scenario_001"), cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("abort: %d", got)
	}
	run, sp, _ := state()
	if run.Warchest != after000.Warchest || sp["Alrek"] != spBefore["Alrek"]-40 {
		t.Errorf("aborting rolled back the funds and the SP: %+v %v", run, sp)
	}
	if _, had, _ := store.MekBayOperation(ctx, id, "refusalwar", "scenario_001"); had {
		t.Error("the battle is gone")
	}
	if done, _ := store.Completions(ctx, id, "refusalwar"); !done["scenario_000"] {
		t.Errorf("the completed mission stays completed: %v", done)
	}
	if len(checkpoints()) != 2 {
		t.Errorf("aborting forgets no checkpoint: %+v", checkpoints())
	}

	// Rolling back to before the mission forgets its checkpoint, deletes its frozen hangar, and reopens the mission.
	gone := cps[0].HangarCopy
	if got := post(t, srv, "/api/campaign/rollback", form("checkpoint", strconv.Itoa(cps[1].Seq)), cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("rollback to the start: %d", got)
	}
	if len(checkpoints()) != 1 || !slices.Contains(fake.deletes, gone) {
		t.Errorf("the later checkpoint is forgotten and its hangar deleted: %+v %v", checkpoints(), fake.deletes)
	}
	if done, _ := store.Completions(ctx, id, "refusalwar"); len(done) != 0 {
		t.Errorf("the mission is open again: %v", done)
	}
	if run, _, _ := state(); run.Warchest != start0.Warchest {
		t.Errorf("and the funds are the starting ones: %+v", run)
	}

	// A MekBay failure leaves everything as it was; bad requests are refused.
	post(t, srv, "/api/warchest", form("funds", "42"), cookie, srv.URL)
	fake.mu.Lock()
	fake.copyErr = http.ErrAbortHandler
	fake.mu.Unlock()
	if got := post(t, srv, "/api/campaign/rollback", form("checkpoint", strconv.Itoa(checkpoints()[0].Seq)), cookie, srv.URL).StatusCode; got != http.StatusBadGateway {
		t.Errorf("a hangar that can't be restored: %d", got)
	}
	if run, _, _ := state(); run.Warchest != 42 {
		t.Errorf("nothing changed: %+v", run)
	}
	fake.mu.Lock()
	fake.copyErr = nil
	fake.mu.Unlock()
	if got := post(t, srv, "/api/campaign/rollback", form("checkpoint", "999"), cookie, srv.URL).StatusCode; got != http.StatusNotFound {
		t.Errorf("no such checkpoint: %d", got)
	}
	if got := post(t, srv, "/api/campaign/rollback", form("checkpoint", "x"), cookie, srv.URL).StatusCode; got != http.StatusBadRequest {
		t.Errorf("not a number: %d", got)
	}
	if got := post(t, srv, "/api/campaign/rollback", form("checkpoint", "1"), cookie, "https://evil.example").StatusCode; got != http.StatusForbidden {
		t.Errorf("cross-origin: %d", got)
	}

	// A campaign keeps at most maxCheckpoints; the oldest go, and their hangars are deleted.
	first := checkpoints()[0].HangarCopy
	for i := 0; i < maxCheckpoints+3; i++ {
		dropped, err := store.AddCheckpoint(ctx, id, "refusalwar", "Filler "+strconv.Itoa(i), "copy-"+strconv.Itoa(i))
		if err != nil {
			t.Fatal(err)
		}
		_ = dropped
	}
	if n := len(checkpoints()); n != maxCheckpoints {
		t.Errorf("at most %d checkpoints: %d", maxCheckpoints, n)
	}
	if cps := checkpoints(); cps[0].Label != "Filler "+strconv.Itoa(maxCheckpoints+2) || slices.ContainsFunc(cps, func(c Checkpoint) bool { return c.HangarCopy == first }) {
		t.Errorf("the newest are kept and the oldest dropped: %+v", cps[0])
	}

	// Deleting the campaign takes its checkpoints with it.
	if got := post(t, srv, "/api/campaign/delete", form(), cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("delete: %d", got)
	}
	if n := len(checkpoints()); n != 0 {
		t.Errorf("the checkpoints go with the campaign: %d", n)
	}
}
