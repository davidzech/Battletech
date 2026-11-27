package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/catalog"
)

// The pages embed the campaign's hangar, so a visit never starts from an empty "reading" state, and the endpoints the scripts call answer the same.
func TestPagesEmbedTheHangar(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	if r := post(t, srv, "/api/account", url.Values{"account": {"Wolf's Dragoons"}}, cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("company: %d", r.StatusCode)
	}
	id := playthroughID("wolfpack2026")
	cachedAttr := func(page string) string {
		const key = `data-hangar="`
		i := strings.Index(page, key)
		if i < 0 {
			return "MISSING"
		}
		rest := page[i+len(key):]
		return rest[:strings.Index(rest, `"`)]
	}

	if r := post(t, srv, "/api/campaign/start", startForm(goodHangar, "Alrek", "Katarina"), cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("start: %d", r.StatusCode)
	}
	// The very first visit to the overview shows the units with no loading state.
	over := body(t, srv, "/refusalwar/", cookie)
	attr := cachedAttr(over)
	if attr == "" || attr == "MISSING" || !strings.Contains(attr, "Mist Lynx") || !strings.Contains(attr, "&#34;bv&#34;:8299") {
		t.Fatalf("the first overview should carry the hangar: %.300s", attr)
	}
	// It is valid JSON for the overview script once unescaped, with the same shape the live endpoint answers with.
	var cached struct {
		Name  string
		Units []hangarUnitView
		BV    int
	}
	unescaped := strings.NewReplacer("&#34;", `"`, "&amp;", "&", "&lt;", "<", "&gt;", ">", "&#39;", "'").Replace(attr)
	if err := json.Unmarshal([]byte(unescaped), &cached); err != nil || len(cached.Units) != 5 || cached.BV != 8299 {
		t.Fatalf("cached payload: %+v %v", cached, err)
	}

	// The Manage page's Mechs list embeds it too, and sits under the pilots (and their memorial) and above the records link.
	man := body(t, srv, "/refusalwar/manage/", cookie)
	mattr := cachedAttr(man)
	i, j, k := strings.Index(man, `id="pilots"`), strings.Index(man, `id="hangar-units"`), strings.Index(man, `id="records-open"`)
	if !strings.Contains(mattr, "Mist Lynx") || !strings.Contains(man, `id="hangar-units-list"`) || !(i >= 0 && i < j && j < k) {
		t.Errorf("the manage page should list the cached mechs under the pilots: attr %.120s, positions %d %d %d", mattr, i, j, k)
	}

	// A change to the hangar shows on the next page, and the overview's endpoint answers it too.
	setRunHangar(t, store, "wolfpack2026", "refusalwar", hangarOf("BMMadCat_H"))
	if attr := cachedAttr(body(t, srv, "/refusalwar/", cookie)); !strings.Contains(attr, "Timber Wolf") || strings.Contains(attr, "Mist Lynx") {
		t.Errorf("the page shows the hangar as it is now: %.300s", attr)
	}
	var live struct{ Units []hangarUnitView }
	if r := get(t, srv, "/api/campaign/force?campaign=refusalwar", cookie); r.StatusCode != http.StatusOK || json.NewDecoder(r.Body).Decode(&live) != nil || len(live.Units) != 1 || live.Units[0].Name != "Timber Wolf" {
		t.Fatalf("the overview's endpoint: %d %+v", r.StatusCode, live)
	}
	if len(fake.reads) != 1 {
		t.Errorf("nothing but the start's link is read from MekBay: %v", fake.reads)
	}

	// The scenario page's deploy popup gets the hangar too, in its own shape (limit, swaps), and its endpoint answers the same.
	setRunHangar(t, store, "wolfpack2026", "refusalwar", startingHangar())
	if r := get(t, srv, "/api/mekbay/hangar?campaign=refusalwar&scenario=scenario_001", cookie); r.StatusCode != http.StatusOK {
		t.Fatalf("deploy popup read: %d", r.StatusCode)
	}
	sattr := cachedAttr(body(t, srv, "/refusalwar/scenario_001/", cookie))
	if !strings.Contains(sattr, "Mist Lynx") || !strings.Contains(sattr, "&#34;limit&#34;:7500") || !strings.Contains(sattr, "&#34;paidSwaps&#34;:true") {
		t.Errorf("the scenario page should carry the hangar for the popup: %.300s", sattr)
	}

	// Deleting the campaign takes its hangar with it.
	if r := post(t, srv, "/api/campaign/delete", url.Values{"campaign": {"refusalwar"}}, cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", r.StatusCode)
	}
	if _, err := store.Hangar(ctx, id, "refusalwar"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("deleting the campaign should delete its hangar: %v", err)
	}
	if r := get(t, srv, "/api/campaign/force?campaign=refusalwar", cookie); r.StatusCode != http.StatusBadRequest {
		t.Errorf("a deleted campaign has no hangar to answer: %d", r.StatusCode)
	}
}

// Each hangar unit carries the link to its own page on mekbay.com (the generic sheet, not a unit in a force).
func TestUnitPageURL(t *testing.T) {
	for in, want := range map[string]string{
		"BMMadCat_H":     "https://mekbay.com/?gs=cbt&shareUnit=BMMadCat_H&tab=General",
		"BMKoshi_E":      "https://mekbay.com/?gs=cbt&shareUnit=BMKoshi_E&tab=General",
		"BMGrizzly":      "https://mekbay.com/?gs=cbt&shareUnit=BMGrizzly&tab=General",
		"BM Odd&Name=1#": "https://mekbay.com/?gs=cbt&shareUnit=BM+Odd%26Name%3D1%23&tab=General",
		"":               "",
	} {
		if got := catalog.PageURL(in); got != want {
			t.Errorf("unitPageURL(%q) = %q, want %q", in, got, want)
		}
	}
	views := hangarViews(hangarOf("BMMadCat_H", "BMNotAUnit"))
	if views[0].MekBayURL != "https://mekbay.com/?gs=cbt&shareUnit=BMMadCat_H&tab=General" || views[1].MekBayURL == "" {
		t.Errorf("hangar views carry the unit's MekBay page: %+v", views)
	}
	b, err := json.Marshal(views[0])
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil || decoded["mekbayUrl"] != "https://mekbay.com/?gs=cbt&shareUnit=BMMadCat_H&tab=General" {
		t.Errorf("the view's JSON should carry mekbayUrl: %v %v", decoded["mekbayUrl"], err)
	}
}
