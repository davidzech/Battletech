package site

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Refusal War allows four living named pilots and charges 300 SP to recruit one (the two starting pilots are free).
func TestRecruitingCostsSP(t *testing.T) {
	srv, store, cookie := startedCompany(t)
	ctx := context.Background()
	id := playthroughID("wolfpack2026")
	c := refusalWar(t)
	if c.PilotLimit() != 4 || c.RecruitCost != 300 {
		t.Fatalf("Refusal War: at most 4 named pilots, 300 SP to recruit: %d %d", c.PilotLimit(), c.RecruitCost)
	}
	funds := func() int { r, _, _ := store.Run(ctx, id, "refusalwar"); return r.Warchest }
	recruit := func(name string) int {
		return post(t, srv, "/api/pilots", url.Values{"campaign": {"refusalwar"}, "name": {name}}, cookie, srv.URL).StatusCode
	}
	start := funds()
	if start != 5000 {
		t.Fatalf("the starting pilots cost nothing: warchest %d", start)
	}
	// The page says what it costs, and the dialog carries the figures.
	page := body(t, srv, "/refusalwar/manage/", cookie)
	if !strings.Contains(page, `<p class="muted recruit-note" id="recruit-note">Recruiting costs 300 SP.</p>`) || !strings.Contains(page, `data-cost="300" data-warchest="5000"`) || strings.Contains(page, `id="recruit-open" disabled`) {
		t.Errorf("an affordable recruit shows its cost: %.200s", page)
	}
	if got := recruit("Jarek"); got != http.StatusNoContent || funds() != start-300 {
		t.Fatalf("recruit: %d, warchest %d (want %d)", got, funds(), start-300)
	}
	// A refused recruit (a taken name) is not charged.
	before := funds()
	if got := recruit("alrek"); got != http.StatusBadRequest || funds() != before {
		t.Errorf("a duplicate name: %d, warchest %d (want %d)", got, funds(), before)
	}
	if got := recruit("Sybille"); got != http.StatusNoContent || funds() != before-300 {
		t.Fatalf("fourth pilot: %d, warchest %d", got, funds())
	}
	// Four living pilots: the button is grayed out with a short note below it, and a fifth recruit is refused without charging.
	before = funds()
	page = body(t, srv, "/refusalwar/manage/", cookie)
	if !strings.Contains(page, `id="recruit-open" disabled`) || !strings.Contains(page, `<p class="muted recruit-note" id="recruit-note">Your command is full: 4 of 4 pilots.</p>`) {
		t.Errorf("a full command: %.200s", page)
	}
	if got := recruit("Fifth"); got != http.StatusBadRequest || funds() != before {
		t.Errorf("a fifth pilot: %d, warchest %d (want %d)", got, funds(), before)
	}
	// Too little in the warchest: refused with 409, the button is disabled and the note says what it costs.
	ps, _ := store.Pilots(ctx, id, "refusalwar")
	if err := store.MarkFallen(ctx, id, "refusalwar", ps[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := store.SetWarchest(ctx, id, "refusalwar", 299); err != nil {
		t.Fatal(err)
	}
	if got := recruit("Newcomer"); got != http.StatusConflict || funds() != 299 {
		t.Errorf("not enough SP: %d, warchest %d", got, funds())
	}
	page = body(t, srv, "/refusalwar/manage/", cookie)
	if !strings.Contains(page, `id="recruit-open" disabled`) || !strings.Contains(page, "Recruiting costs 300 SP (warchest: 299).") {
		t.Errorf("too poor to recruit: %.200s", page)
	}
	if err := store.SetWarchest(ctx, id, "refusalwar", 300); err != nil {
		t.Fatal(err)
	}
	if got := recruit("Newcomer"); got != http.StatusNoContent || funds() != 0 {
		t.Errorf("exactly 300 SP: %d, warchest %d", got, funds())
	}
}
