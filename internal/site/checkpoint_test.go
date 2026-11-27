package site

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/pilot"
	"github.com/davidzech/Battletech/internal/storage"
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
	state := func() (run storage.CampaignRun, sp map[string]int, fallen map[string]bool) {
		run, _, _ = store.Run(ctx, id, "refusalwar")
		sp, fallen = map[string]int{}, map[string]bool{}
		ps, _ := store.Pilots(ctx, id, "refusalwar")
		for _, p := range ps {
			sp[p.Name], fallen[p.Name] = p.TotalSP, p.Fallen
		}
		return
	}
	checkpoints := func() []storage.Checkpoint {
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

	// Starting the campaign is the first checkpoint, the hangar in it.
	cps := checkpoints()
	if len(cps) != 1 || cps[0].Label != "Campaign started" {
		t.Fatalf("the starting checkpoint: %+v", cps)
	}
	start0, sp0, _ := state()
	units := func() int { return len(runHangar(t, store, seed, "refusalwar").Units) }

	// Rolling back is offered from the campaign page's History, on the line the checkpoint follows; the Manage page no longer has a Rollback of its own.
	if p := body(t, srv, "/refusalwar/", cookie); !strings.Contains(p, `data-checkpoint="`+strconv.Itoa(cps[0].Seq)+`" data-label="Campaign started"`) {
		t.Error("the History offers the starting checkpoint")
	}
	if p := body(t, srv, "/refusalwar/manage/", cookie); strings.Contains(p, "rollback-open") || strings.Contains(p, "rollback-dialog") {
		t.Error("the Manage page has no Rollback of its own")
	}

	// Change things: the hangar (a mech sold), the warchest, a pilot's SP, a pilot killed in action. None of it makes a checkpoint.
	if got := post(t, srv, "/api/hangar/sell", form("unit", "ua"), cookie, srv.URL).StatusCode; got != http.StatusOK || units() != 4 {
		t.Fatalf("sell: %d, %d units", got, units())
	}
	post(t, srv, "/api/warchest", form("funds", "777"), cookie, srv.URL)
	post(t, srv, "/api/pilots/allocate", form("pilot", pilotID["Alrek"], "earned", "90"), cookie, srv.URL)
	post(t, srv, "/api/pilots/fallen", form("pilot", pilotID["Katarina"]), cookie, srv.URL)
	if run, sp, fallen := state(); run.Warchest != 777 || sp["Alrek"] != sp0["Alrek"]+90 || !fallen["Katarina"] || len(checkpoints()) != 1 {
		t.Fatalf("the changes took: %+v %v %v", run, sp, fallen)
	}

	// Rolling back to the starting checkpoint puts the hangar, the warchest, the pilots' SP and who is alive back.
	if got := post(t, srv, "/api/campaign/rollback", form("checkpoint", strconv.Itoa(cps[0].Seq)), cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("rollback: %d", got)
	}
	if run, sp, fallen := state(); run.Warchest != start0.Warchest || sp["Alrek"] != sp0["Alrek"] || fallen["Katarina"] || units() != 5 {
		t.Errorf("rolled back: %+v %v %v, %d units", run, sp, fallen, units())
	}

	// Completing a mission is a checkpoint: the state after it, with the mission completed and its rewards in.
	if got := post(t, srv, "/api/campaign/scenario-outcome/commit", url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_000"}, "inputs": {`{"pilots":[],"mission":{}}`}}, cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("complete 000: %d", got)
	}
	cps = checkpoints()
	if len(cps) != 2 || cps[0].Label != "Completed Scenario 000: Trial of Position" {
		t.Fatalf("the mission's checkpoint: %+v", cps)
	}
	after000, _, _ := state()

	// Between missions, things change (a pilot's SP, the funds), and a battle starts for the next mission.
	post(t, srv, "/api/pilots/allocate", form("pilot", pilotID["Alrek"], "earned", "40"), cookie, srv.URL)
	post(t, srv, "/api/warchest", form("funds", "1"), cookie, srv.URL)
	if err := store.SaveMekBayOperation(ctx, id, "refusalwar", "scenario_001", storage.MekBayOperation{OpForInstance: "opfor", OperationID: "op-1"}); err != nil {
		t.Fatal(err)
	}
	_, spBefore, _ := state()
	// Aborting the mission rolls back to the end of the previous one: the funds and SP are back, the battle is gone, the mission-000 completion stays.
	if got := post(t, srv, "/api/battle/abort", form("scenario", "scenario_001", "mode", "rollback"), cookie, srv.URL).StatusCode; got != http.StatusNoContent {
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

	// Rolling back to before the mission forgets its checkpoint and reopens the mission.
	if got := post(t, srv, "/api/campaign/rollback", form("checkpoint", strconv.Itoa(cps[1].Seq)), cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("rollback to the start: %d", got)
	}
	if len(checkpoints()) != 1 {
		t.Errorf("the later checkpoint is forgotten: %+v", checkpoints())
	}
	if done, _ := store.Completions(ctx, id, "refusalwar"); len(done) != 0 {
		t.Errorf("the mission is open again: %v", done)
	}
	if run, _, _ := state(); run.Warchest != start0.Warchest {
		t.Errorf("and the funds are the starting ones: %+v", run)
	}

	// Bad requests are refused.
	if got := post(t, srv, "/api/campaign/rollback", form("checkpoint", "999"), cookie, srv.URL).StatusCode; got != http.StatusNotFound {
		t.Errorf("no such checkpoint: %d", got)
	}
	if got := post(t, srv, "/api/campaign/rollback", form("checkpoint", "x"), cookie, srv.URL).StatusCode; got != http.StatusBadRequest {
		t.Errorf("not a number: %d", got)
	}
	if got := post(t, srv, "/api/campaign/rollback", form("checkpoint", "1"), cookie, "https://evil.example").StatusCode; got != http.StatusForbidden {
		t.Errorf("cross-origin: %d", got)
	}

	// A campaign keeps at most maxCheckpoints; the oldest go.
	first := checkpoints()[0].Seq
	for i := 0; i < storage.MaxCheckpoints+3; i++ {
		if err := store.AddCheckpoint(ctx, id, "refusalwar", "Filler "+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(checkpoints()); n != storage.MaxCheckpoints {
		t.Errorf("at most %d checkpoints: %d", storage.MaxCheckpoints, n)
	}
	if cps := checkpoints(); cps[0].Label != "Filler "+strconv.Itoa(storage.MaxCheckpoints+2) || slices.ContainsFunc(cps, func(c storage.Checkpoint) bool { return c.Seq == first }) {
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

// Advanced Management's corrections (rename, delete, revive, reset, override the total, set the funds) are changes to the live campaign like any other: a rollback puts
// everything back as it was at the checkpoint, including a pilot deleted since, a name changed, and an SP total overridden.
func TestRollbackUndoesAdvancedManagement(t *testing.T) {
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
	pilots := func() map[string]pilot.NamedPilot {
		ps, _ := store.Pilots(ctx, id, "refusalwar")
		out := map[string]pilot.NamedPilot{}
		for _, p := range ps {
			out[p.ID] = p
		}
		return out
	}
	before := pilots()
	var alrek, kat string
	for pid, p := range before {
		if p.Name == "Alrek" {
			alrek = pid
		} else {
			kat = pid
		}
	}
	cps, _ := store.Checkpoints(ctx, id, "refusalwar")

	post(t, srv, "/api/pilots/rename", form("pilot", alrek, "name", "Renamed"), cookie, srv.URL)
	post(t, srv, "/api/pilots/total", form("pilot", alrek, "total", "9000"), cookie, srv.URL)
	post(t, srv, "/api/pilots/reset", form("pilot", kat), cookie, srv.URL)
	post(t, srv, "/api/pilots/fallen", form("pilot", kat), cookie, srv.URL)
	post(t, srv, "/api/pilots/delete", form("pilot", kat), cookie, srv.URL)
	post(t, srv, "/api/warchest", form("funds", "9000"), cookie, srv.URL)  // enough to recruit
	post(t, srv, "/api/pilots", form("name", "Newcomer"), cookie, srv.URL) // a recruit after the checkpoint
	if p := pilots(); p[alrek].Name != "Renamed" || p[alrek].TotalSP != 9000 || len(p) != 2 {
		t.Fatalf("the corrections took: %+v", p)
	}

	if got := post(t, srv, "/api/campaign/rollback", form("checkpoint", strconv.Itoa(cps[0].Seq)), cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("rollback: %d", got)
	}
	after := pilots()
	if len(after) != len(before) {
		t.Fatalf("the same pilots as at the checkpoint (the recruit is gone, the deleted one is back): %+v", after)
	}
	for pid, want := range before {
		got := after[pid]
		if got.Name != want.Name || got.TotalSP != want.TotalSP || got.SP != want.SP || got.Fallen != want.Fallen || got.Portrait != want.Portrait || got.Rank != want.Rank {
			t.Errorf("%s is as at the checkpoint: %+v (was %+v)", want.Name, got, want)
		}
	}
	if run, _, _ := store.Run(ctx, id, "refusalwar"); run.Warchest != 5000 {
		t.Errorf("the funds are back: %d", run.Warchest)
	}
}

// Aborting a mission has two ways out: "rollback" undoes everything since the last checkpoint; "keep" (the default) only closes the battle, cleaning up its operation
// and forces in MekBay, and keeps every change.
func TestAbortModes(t *testing.T) {
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
		v := url.Values{"campaign": {"refusalwar"}, "scenario": {"scenario_000"}}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Set(kv[i], kv[i+1])
		}
		return v
	}
	battle := func(op string) {
		if err := store.SaveMekBayOperation(ctx, id, "refusalwar", "scenario_000", storage.MekBayOperation{OpForInstance: "opfor", OperationID: op}); err != nil {
			t.Fatal(err)
		}
	}
	warchest := func() int { r, _, _ := store.Run(ctx, id, "refusalwar"); return r.Warchest }
	_, _, _ = store, ctx, id

	// "keep": the battle is closed and cleaned up, the changes stay.
	post(t, srv, "/api/warchest", url.Values{"campaign": {"refusalwar"}, "funds": {"321"}}, cookie, srv.URL)
	battle("op-keep")
	if got := post(t, srv, "/api/battle/abort", form("mode", "keep"), cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatalf("abort (keep): %d", got)
	}
	if _, had, _ := store.MekBayOperation(ctx, id, "refusalwar", "scenario_000"); had || warchest() != 321 {
		t.Errorf("keep: the battle is gone and the funds stay: %v %d", had, warchest())
	}
	fake.mu.Lock()
	cleaned := slices.Clone(fake.battleDeletes)
	fake.mu.Unlock()
	if !slices.Contains(cleaned, "op-keep") {
		t.Errorf("the operation and its forces are cleaned up in MekBay: %v", cleaned)
	}

	// no mode means keep, too
	battle("op-default")
	if got := post(t, srv, "/api/battle/abort", form(), cookie, srv.URL).StatusCode; got != http.StatusNoContent || warchest() != 321 {
		t.Errorf("the default keeps the changes: %d %d", got, warchest())
	}

	// "rollback": back to the starting checkpoint, and cleaned up as well.
	battle("op-roll")
	if got := post(t, srv, "/api/battle/abort", form("mode", "rollback"), cookie, srv.URL).StatusCode; got != http.StatusNoContent || warchest() != 5000 {
		t.Errorf("rollback: %d, funds %d", got, warchest())
	}
	if h, _ := store.History(ctx, id, "refusalwar"); len(h) == 0 || h[len(h)-1].Kind != storage.HistoryMission || !strings.HasPrefix(h[len(h)-1].Text, "Aborted Scenario 000") || !strings.Contains(h[len(h)-1].Text, "and rolled back to \u201c") {
		t.Errorf("an abort with rollback says so: %+v", h)
	}
	fake.mu.Lock()
	cleaned = slices.Clone(fake.battleDeletes)
	fake.mu.Unlock()
	if !slices.Contains(cleaned, "op-roll") {
		t.Errorf("a rolled-back battle is cleaned up too: %v", cleaned)
	}

	// anything else is refused, and changes nothing
	battle("op-bad")
	if got := post(t, srv, "/api/battle/abort", form("mode", "explode"), cookie, srv.URL).StatusCode; got != http.StatusBadRequest {
		t.Errorf("a bad mode: %d", got)
	}
	if _, had, _ := store.MekBayOperation(ctx, id, "refusalwar", "scenario_000"); !had {
		t.Error("a refused abort leaves the battle")
	}
}

// Keywords are in the snapshot: a rollback takes back the keywords granted since the checkpoint and keeps the ones it already held.
func TestRollbackRestoresKeywords(t *testing.T) {
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
	grant := func(kw string) {
		if _, err := store.DB().ExecContext(ctx, store.Rebind(`INSERT INTO campaign_keywords (playthrough_id, campaign, keyword, scenario_id) VALUES (?, ?, ?, ?)`), id, "refusalwar", kw, "scenario_000"); err != nil {
			t.Fatal(err)
		}
	}
	keywords := func() string {
		l, _ := store.Ledger(ctx, id, "refusalwar")
		var out []string
		for _, e := range l {
			out = append(out, e.Keyword)
		}
		slices.Sort(out)
		return strings.Join(out, ",")
	}
	grant("first_blood")
	if err := store.AddCheckpoint(ctx, id, "refusalwar", "After the first mission"); err != nil { // a checkpoint that holds first_blood
		t.Fatal(err)
	}
	cps, _ := store.Checkpoints(ctx, id, "refusalwar")
	grant("depot_gone")
	if keywords() != "depot_gone,first_blood" {
		t.Fatalf("both are held: %s", keywords())
	}
	rb := func(seq int) int {
		return post(t, srv, "/api/campaign/rollback", url.Values{"campaign": {"refusalwar"}, "checkpoint": {strconv.Itoa(seq)}}, cookie, srv.URL).StatusCode
	}
	if got := rb(cps[0].Seq); got != http.StatusNoContent || keywords() != "first_blood" {
		t.Errorf("the keyword granted since is taken back, the earlier one stays: %d %q", got, keywords())
	}
	cps, _ = store.Checkpoints(ctx, id, "refusalwar")
	if got := rb(cps[len(cps)-1].Seq); got != http.StatusNoContent || keywords() != "" {
		t.Errorf("back at the start no keyword is held: %d %q", got, keywords())
	}
}

// A rollback whose rows can't be restored leaves the campaign as it was, its hangar too (one transaction), and the player is told nothing of the database's
// error.
func TestRollbackFailureChangesNothing(t *testing.T) {
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
	cps, _ := store.Checkpoints(ctx, id, "refusalwar")
	post(t, srv, "/api/warchest", url.Values{"campaign": {"refusalwar"}, "funds": {"42"}}, cookie, srv.URL)
	setRunHangar(t, store, seed, "refusalwar", hangarOf("BMMadCat_H"))

	// The checkpoint's snapshot names a column the table doesn't have (as one taken before a column was dropped would), so restoring it fails.
	broken := `{"campaign_runs":[{"playthrough_id":"` + id + `","campaign":"refusalwar","hangar":"{}","no_such_column":1}]}`
	if _, err := store.DB().ExecContext(ctx, store.Rebind(`UPDATE campaign_checkpoints SET snapshot = ? WHERE playthrough_id = ? AND campaign = ?`), broken, id, "refusalwar"); err != nil {
		t.Fatal(err)
	}
	res := post(t, srv, "/api/campaign/rollback", url.Values{"campaign": {"refusalwar"}, "checkpoint": {strconv.Itoa(cps[0].Seq)}}, cookie, srv.URL)
	msg, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusInternalServerError || strings.TrimSpace(string(msg)) != "Could not roll the campaign back." {
		t.Errorf("a database failure answers a fixed message: %d %q", res.StatusCode, msg)
	}
	if now, _, _ := store.Run(ctx, id, "refusalwar"); now.Warchest != 42 {
		t.Errorf("the rows are as they were: %+v", now)
	}
	if h := runHangar(t, store, seed, "refusalwar"); len(h.Units) != 1 || h.Units[0].Unit != "BMMadCat_H" {
		t.Errorf("the hangar is as it was: %+v", h)
	}
	if left, _ := store.Checkpoints(ctx, id, "refusalwar"); len(left) != 1 {
		t.Errorf("the checkpoint is still there to try again: %+v", left)
	}
	if h, _ := store.History(ctx, id, "refusalwar"); len(h) == 0 || strings.Contains(h[len(h)-1].Text, "Rolled back") {
		t.Errorf("a failed rollback is not in the history: %+v", h)
	}
}
