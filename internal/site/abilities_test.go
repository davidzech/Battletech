package site

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/pilot"
)

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
