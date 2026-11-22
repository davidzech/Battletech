package site

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/pilot"
)

// abilityStore checks choosing and resetting Edge abilities on both databases, in a campaign of its own (run from storeContract).
func abilityStore(t *testing.T, s *Store, id string) {
	t.Helper()
	ctx := context.Background()
	const camp = "skillcamp"
	// 180 SP in Edge abilities is level 2, so two abilities to choose.
	a, err := s.AddPilot(ctx, id, camp, "Alrek", "", pilot.SPAllocation{EdgeAbilities: 180}, 4)
	if err != nil {
		t.Fatal(err)
	}
	get := func() pilot.NamedPilot {
		ps, _ := s.Pilots(ctx, id, camp)
		for _, p := range ps {
			if p.ID == a.ID {
				return p
			}
		}
		t.Fatal("pilot vanished")
		return pilot.NamedPilot{}
	}
	if p := get(); p.EdgeAbilities() != 2 || p.PendingAbilities() != 2 || len(p.Learned) != 0 {
		t.Fatalf("two abilities earned, none chosen: %+v", p)
	}
	for name, c := range map[string]struct {
		ids  []string
		want error
	}{
		"none":           {nil, ErrAbilityCount},
		"too few":        {[]string{"marksman"}, ErrAbilityCount},
		"too many":       {[]string{"marksman", "nimble", "patient"}, ErrAbilityCount},
		"unknown":        {[]string{"marksman", "nope"}, ErrUnknownAbility},
		"the same twice": {[]string{"marksman", "marksman"}, ErrAbilityLearned},
	} {
		if err := s.LearnAbilities(ctx, id, camp, a.ID, c.ids); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
	if p := get(); len(p.Learned) != 0 {
		t.Fatalf("refused choices changed the pilot: %+v", p.Learned)
	}
	if err := s.LearnAbilities(ctx, id, camp, a.ID, []string{"nimble", "marksman"}); err != nil {
		t.Fatal(err)
	}
	if p := get(); strings.Join(p.Learned, ",") != "nimble,marksman" || p.PendingAbilities() != 0 {
		t.Fatalf("learned in the order chosen: %+v", p.Learned)
	}
	if err := s.LearnAbilities(ctx, id, camp, a.ID, []string{"patient"}); !errors.Is(err, ErrNoPendingAbilities) {
		t.Fatalf("nothing pending: %v", err)
	}
	// A new level brings a new choice; an ability can only be learned once.
	if err := s.AwardSP(ctx, id, camp, a.ID, pilot.SPAllocation{EdgeAbilities: 180}); err != nil { // 360: level 3
		t.Fatal(err)
	}
	if p := get(); p.PendingAbilities() != 1 {
		t.Fatalf("level 3 with two learned: %d pending", p.PendingAbilities())
	}
	if err := s.LearnAbilities(ctx, id, camp, a.ID, []string{"marksman"}); !errors.Is(err, ErrAbilityLearned) {
		t.Fatalf("already learned: %v", err)
	}
	if err := s.LearnAbilities(ctx, id, camp, a.ID, []string{"bulwark"}); err != nil {
		t.Fatal(err)
	}
	if p := get(); strings.Join(p.Learned, ",") != "nimble,marksman,bulwark" {
		t.Fatalf("order after a later choice: %+v", p.Learned)
	}
	// Resetting the SP allocation forgets the learned abilities too: they depended on the Edge abilities SP that has just been taken back.
	if err := s.ResetPilotSP(ctx, id, camp, a.ID); err != nil {
		t.Fatal(err)
	}
	if p := get(); len(p.Learned) != 0 || p.EdgeAbilities() != 0 || p.PendingAbilities() != 0 {
		t.Fatalf("an SP reset forgets learned abilities: %+v level %d pending %d", p.Learned, p.EdgeAbilities(), p.PendingAbilities())
	}
	// Setting the total: at or above the allocation touches nothing, refused below it, and below it with the reset agreed forgets them.
	if err := s.AwardSP(ctx, id, camp, a.ID, pilot.SPAllocation{EdgeAbilities: 360}); err != nil { // level 3
		t.Fatal(err)
	}
	if err := s.LearnAbilities(ctx, id, camp, a.ID, []string{"nimble", "marksman", "patient"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPilotTotal(ctx, id, camp, a.ID, 1000, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPilotTotal(ctx, id, camp, a.ID, 100, false); !errors.Is(err, ErrTotalBelowAllocated) {
		t.Fatalf("below the allocation: %v", err)
	}
	if p := get(); len(p.Learned) != 3 {
		t.Fatalf("a raised or refused total must keep the abilities: %+v", p.Learned)
	}
	if err := s.SetPilotTotal(ctx, id, camp, a.ID, 100, true); err != nil {
		t.Fatal(err)
	}
	if p := get(); len(p.Learned) != 0 || p.SP.Total() != 0 || p.TotalSP != 100 {
		t.Fatalf("a total below the allocation, with the reset, forgets the abilities: %+v", p)
	}
	// The explicit reset forgets them, for the pilot to choose again.
	if err := s.AwardSP(ctx, id, camp, a.ID, pilot.SPAllocation{EdgeAbilities: 360}); err != nil {
		t.Fatal(err)
	}
	if err := s.LearnAbilities(ctx, id, camp, a.ID, []string{"nimble", "marksman", "patient"}); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetEdgeAbilities(ctx, id, camp, a.ID); err != nil {
		t.Fatal(err)
	}
	if p := get(); len(p.Learned) != 0 || p.PendingAbilities() != 3 {
		t.Fatalf("after the reset: %+v pending %d", p.Learned, p.PendingAbilities())
	}
	if err := s.LearnAbilities(ctx, id, camp, "nobody", []string{"nimble"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown pilot: %v", err)
	}
	if err := s.ResetEdgeAbilities(ctx, id, camp, "nobody"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("reset of an unknown pilot: %v", err)
	}
	if err := s.LearnAbilities(ctx, id, "another", a.ID, []string{"nimble", "marksman", "patient"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("another campaign's pilot: %v", err)
	}
	// A pilot Killed in Action can't choose, but their abilities can still be reset.
	if err := s.LearnAbilities(ctx, id, camp, a.ID, []string{"nimble", "marksman", "patient"}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkFallen(ctx, id, camp, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.LearnAbilities(ctx, id, camp, a.ID, []string{"cautious"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("the fallen choose nothing: %v", err)
	}
	count := func() int {
		var n int
		if err := s.db.QueryRow(s.q(`SELECT COUNT(*) FROM pilot_abilities WHERE pilot_id = ?`), a.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count() != 3 {
		t.Fatalf("learned rows: %d", count())
	}
	if err := s.ResetEdgeAbilities(ctx, id, camp, a.ID); err != nil || count() != 0 {
		t.Fatalf("reset a fallen pilot: %v %d", err, count())
	}
	// Deleting a pilot or a whole campaign takes their abilities with it.
	if err := s.LearnAbilities(ctx, id, camp, a.ID, []string{"nimble"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("still fallen")
	}
	b, err := s.AddPilot(ctx, id, camp, "Katarina", "", pilot.SPAllocation{EdgeAbilities: 60}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LearnAbilities(ctx, id, camp, b.ID, []string{"patient"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePilot(ctx, id, camp, b.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(s.q(`SELECT COUNT(*) FROM pilot_abilities WHERE pilot_id = ?`), b.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a deleted pilot's abilities: %d %v", n, err)
	}
	c, err := s.AddPilot(ctx, id, camp, "Jarek", "", pilot.SPAllocation{EdgeAbilities: 60}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LearnAbilities(ctx, id, camp, c.ID, []string{"cautious"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteRun(ctx, id, camp); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(s.q(`SELECT COUNT(*) FROM pilot_abilities WHERE pilot_id = ?`), c.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a deleted campaign's abilities: %d %v", n, err)
	}
}

func TestEdgeAbilitiesThroughThePages(t *testing.T) {
	srv, store, cookie := startedCompany(t)
	ctx := context.Background()
	id := playthroughID("wolfpack2026")
	ps, _ := store.Pilots(ctx, id, "refusalwar")
	pl := ps[0]
	learn := func(pilotID string, ids ...string) int {
		v := url.Values{"campaign": {"refusalwar"}, "pilot": {pilotID}}
		for _, a := range ids {
			v.Add("ability", a)
		}
		return post(t, srv, "/api/pilots/abilities", v, cookie, srv.URL).StatusCode
	}
	page := func() string { return body(t, srv, "/refusalwar/manage/", cookie) }

	// A pilot with no Edge abilities level has nothing to choose, but the picker is on the page, with the catalog.
	p := page()
	for _, want := range []string{`id="skill-dialog"`, `id="skill-slots"`, `data-abilities="`, `&#34;id&#34;:&#34;marksman&#34;`, `class="rec-skills-reset" disabled`, ">Reset SP allocation<", ">Reset Edge abilities<"} {
		if !strings.Contains(p, want) {
			t.Errorf("the manage page is missing %q", want)
		}
	}
	if strings.Contains(p, `class="button skill-pick"`) || strings.Contains(p, ">Reset abilities<") {
		t.Error("nothing to choose yet, and the SP reset has been renamed")
	}
	if got := learn(pl.ID, "marksman"); got != http.StatusBadRequest {
		t.Errorf("choosing with nothing earned: %d", got)
	}

	// Level 1 in Edge abilities: one ability to choose.
	if err := store.AwardSP(ctx, id, "refusalwar", pl.ID, pilot.SPAllocation{EdgeAbilities: 60}); err != nil {
		t.Fatal(err)
	}
	p = page()
	if !strings.Contains(p, `class="button skill-pick">Choose a new Edge ability</button>`) || !strings.Contains(p, `data-edge-pending="1"`) {
		t.Error("a pilot with a new Edge abilities level is offered a choice")
	}
	if got := post(t, srv, "/api/pilots/abilities", url.Values{"campaign": {"refusalwar"}, "pilot": {pl.ID}, "ability": {"marksman"}}, cookie, "https://evil.example").StatusCode; got != http.StatusForbidden {
		t.Errorf("cross-origin: %d", got)
	}
	for name, c := range map[string][]string{
		"none":    nil,
		"two":     {"marksman", "nimble"},
		"unknown": {"nope"},
		"blank":   {""},
	} {
		if got := learn(pl.ID, c...); got != http.StatusBadRequest {
			t.Errorf("%s: %d", name, got)
		}
	}
	if got := learn("nobody", "marksman"); got != http.StatusNotFound {
		t.Errorf("unknown pilot: %d", got)
	}
	if got := learn(pl.ID, "marksman"); got != http.StatusNoContent {
		t.Fatalf("choose: %d", got)
	}
	p = page()
	if !strings.Contains(p, `>Marksman</span>`) || strings.Contains(p, `class="button skill-pick"`) || !strings.Contains(p, `data-learned="marksman "`) || !strings.Contains(p, "<b class=\"rec-n-skills\">Marksman</b>") {
		t.Error("the chosen ability shows as a chip, the choice is gone, and the records row lists it")
	}
	if got := learn(pl.ID, "nimble"); got != http.StatusBadRequest {
		t.Errorf("nothing pending any more: %d", got)
	}
	// The chip names its ability for the hover card script, and carries no native tooltip (that would double up with ours).
	if !strings.Contains(p, `class="skill-chip" tabindex="0" data-ability="marksman" aria-describedby="skill-tip">Marksman</span>`) || strings.Contains(p, `class="skill-chip" tabindex="0" title=`) {
		t.Error("a learned ability's chip should name its ability and have no title attribute")
	}

	// More levels, more choices at once; each only once.
	if err := store.AwardSP(ctx, id, "refusalwar", pl.ID, pilot.SPAllocation{EdgeAbilities: 540}); err != nil { // 600: level 4
		t.Fatal(err)
	}
	if !strings.Contains(page(), "Choose 3 new Edge abilities") {
		t.Error("three more to choose")
	}
	for name, c := range map[string][]string{
		"already learned": {"marksman", "nimble", "patient"},
		"the same twice":  {"nimble", "nimble", "patient"},
		"too few":         {"nimble", "patient"},
	} {
		if got := learn(pl.ID, c...); got != http.StatusBadRequest {
			t.Errorf("%s: %d", name, got)
		}
	}
	if got := learn(pl.ID, "nimble", "patient", "bulwark"); got != http.StatusNoContent {
		t.Fatalf("choose three: %d", got)
	}
	ps, _ = store.Pilots(ctx, id, "refusalwar")
	if strings.Join(ps[0].Learned, ",") != "marksman,nimble,patient,bulwark" {
		t.Fatalf("learned: %v", ps[0].Learned)
	}

	// A pilot Killed in Action shows their abilities in the memorial and can have them reset.
	if got := post(t, srv, "/api/pilots/fallen", url.Values{"campaign": {"refusalwar"}, "pilot": {pl.ID}}, cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatal(got)
	}
	p = page()
	i := strings.Index(p, `class="memorial-head"`)
	if i < 0 || !strings.Contains(p[i:], `class="kia-stamp">K.I.A.</span>`) {
		t.Error("a pilot in the memorial carries a K.I.A. stamp over their portrait")
	}
	if i < 0 || !strings.Contains(p[i:], `>Marksman</span>`) || !strings.Contains(p[i:], `>Bulwark</span>`) {
		t.Error("the memorial should show the abilities as chips")
	}
	reset := func(pid string) int {
		return post(t, srv, "/api/pilots/abilities/reset", url.Values{"campaign": {"refusalwar"}, "pilot": {pid}}, cookie, srv.URL).StatusCode
	}
	if got := post(t, srv, "/api/pilots/abilities/reset", url.Values{"campaign": {"refusalwar"}, "pilot": {pl.ID}}, cookie, "https://evil.example").StatusCode; got != http.StatusForbidden {
		t.Errorf("cross-origin reset: %d", got)
	}
	if got := reset("nobody"); got != http.StatusNotFound {
		t.Errorf("reset of an unknown pilot: %d", got)
	}
	if got := reset(pl.ID); got != http.StatusNoContent {
		t.Fatalf("reset: %d", got)
	}
	if ps, _ := store.Pilots(ctx, id, "refusalwar"); len(ps[0].Learned) != 0 {
		t.Errorf("after the reset: %v", ps[0].Learned)
	}
	// Revived, the pilot is owed the choices again, since their level is untouched.
	if got := post(t, srv, "/api/pilots/revive", url.Values{"campaign": {"refusalwar"}, "pilot": {pl.ID}}, cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatal(got)
	}
	if !strings.Contains(page(), "Choose 4 new Edge abilities") {
		t.Error("after a reset the pilot chooses all four again")
	}
}

// Resetting a pilot's SP allocation, by the button or by setting a total below it, forgets their Edge abilities too.
func TestSPResetForgetsEdgeAbilities(t *testing.T) {
	srv, store, cookie := startedCompany(t)
	ctx := context.Background()
	id := playthroughID("wolfpack2026")
	ps, _ := store.Pilots(ctx, id, "refusalwar")
	pl := ps[0]
	form := func(kv ...string) url.Values {
		v := url.Values{"campaign": {"refusalwar"}, "pilot": {pl.ID}}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Set(kv[i], kv[i+1])
		}
		return v
	}
	setUp := func() {
		t.Helper()
		if err := store.AwardSP(ctx, id, "refusalwar", pl.ID, pilot.SPAllocation{EdgeAbilities: 60}); err != nil {
			t.Fatal(err)
		}
		if err := store.LearnAbilities(ctx, id, "refusalwar", pl.ID, []string{"marksman"}); err != nil {
			t.Fatal(err)
		}
	}
	learned := func() int {
		ps, _ := store.Pilots(ctx, id, "refusalwar")
		return len(ps[0].Learned)
	}
	setUp()
	if learned() != 1 {
		t.Fatal("set up")
	}
	// A total that keeps the allocation, or one refused for being below it, leaves the ability alone.
	if got := post(t, srv, "/api/pilots/total", form("total", "900"), cookie, srv.URL).StatusCode; got != http.StatusNoContent {
		t.Fatal(got)
	}
	if got := post(t, srv, "/api/pilots/total", form("total", "10"), cookie, srv.URL).StatusCode; got != http.StatusConflict || learned() != 1 {
		t.Fatalf("refused total: %d, learned %d", got, learned())
	}
	// Agreeing to the reset forgets it.
	if got := post(t, srv, "/api/pilots/total", form("total", "10", "reset", "1"), cookie, srv.URL).StatusCode; got != http.StatusNoContent || learned() != 0 {
		t.Fatalf("total with reset: %d, learned %d", got, learned())
	}
	// So does the Reset SP allocation button.
	setUp()
	if learned() != 1 {
		t.Fatal("set up again")
	}
	if got := post(t, srv, "/api/pilots/reset", form(), cookie, srv.URL).StatusCode; got != http.StatusNoContent || learned() != 0 {
		t.Fatalf("reset: %d, learned %d", got, learned())
	}
	if ps, _ := store.Pilots(ctx, id, "refusalwar"); ps[0].EdgeAbilities() != 0 || ps[0].PendingAbilities() != 0 {
		t.Errorf("after the reset the pilot has no Edge abilities level and nothing pending: %+v", ps[0])
	}
}
