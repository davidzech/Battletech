package site

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// hangarCacheStore checks the cache table on both databases, in a campaign of its own (run from storeContract).
func hangarCacheStore(t *testing.T, s *Store, id string) {
	t.Helper()
	ctx := context.Background()
	const camp, inst = "cachecamp", "01a12f9e-188f-7158-ad67-3fc9700ec733"
	if _, _, ok, err := s.CachedHangar(ctx, id, camp, inst); err != nil || ok {
		t.Fatalf("nothing cached yet: %v %v", ok, err)
	}
	h := hangarOf("BMKoshi_E", "BMGrizzly")
	h.Units[1].Pilot, h.Units[1].Damaged = "Alrek", true
	if err := s.SaveHangarCache(ctx, id, camp, inst, h); err != nil {
		t.Fatal(err)
	}
	got, at, ok, err := s.CachedHangar(ctx, id, camp, inst)
	if err != nil || !ok || got.Name != "Test Hangar" || len(got.Units) != 2 || got.Units[1] != h.Units[1] || at.IsZero() {
		t.Fatalf("cached hangar: %+v at %v ok %v err %v", got, at, ok, err)
	}
	// A copy belongs to the hangar link it was read from.
	if _, _, ok, _ := s.CachedHangar(ctx, id, camp, "another-instance"); ok {
		t.Error("a different hangar link must not see the old copy")
	}
	// Saving again replaces it (one row per run), including its link.
	h2 := hangarOf("BMMadCat_H")
	if err := s.SaveHangarCache(ctx, id, camp, "another-instance", h2); err != nil {
		t.Fatal(err)
	}
	if got, _, ok, _ := s.CachedHangar(ctx, id, camp, "another-instance"); !ok || len(got.Units) != 1 || got.Units[0].Unit != "BMMadCat_H" {
		t.Errorf("the copy should have been replaced: %+v", got)
	}
	if _, _, ok, _ := s.CachedHangar(ctx, id, camp, inst); ok {
		t.Error("the replaced copy's old link is gone")
	}
	// Other campaigns are separate, and deleting the run clears its copy.
	if _, _, ok, _ := s.CachedHangar(ctx, id, "othercamp", "another-instance"); ok {
		t.Error("another campaign's cache")
	}
	if _, err := s.DeleteRun(ctx, id, camp); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, _ := s.CachedHangar(ctx, id, camp, "another-instance"); ok {
		t.Error("deleting the run should clear its cached hangar")
	}
}

// The pages paint the last hangar read from MekBay straight away, so a visit never starts from an empty "reading" state.
func TestHangarCachePullThrough(t *testing.T) {
	fake := &fakeMekBay{hangar: startingHangar()}
	srv, store := newMekBayTestServer(t, fake)
	ctx := context.Background()
	cookie := post(t, srv, "/api/seed", url.Values{"seed": {"wolfpack2026"}}, nil, "").Cookies()[0]
	if r := post(t, srv, "/api/company", url.Values{"company": {"Wolf's Dragoons"}}, cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("company: %d", r.StatusCode)
	}
	id := playthroughID("wolfpack2026")
	cachedAttr := func(page string) string {
		const key = `data-cached="`
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
	// Starting read the hangar for its check, and that read is already the run's first cached copy: the very first visit to the
	// overview shows the units with no loading state.
	over := body(t, srv, "/refusalwar/", cookie)
	attr := cachedAttr(over)
	if attr == "" || attr == "MISSING" || !strings.Contains(attr, "Mist Lynx") || !strings.Contains(attr, "&#34;bv&#34;:5645") || !strings.Contains(attr, "cachedAt") {
		t.Fatalf("the first overview should carry the cached hangar: %.300s", attr)
	}
	// It is valid JSON for the overview script once unescaped, with the same shape the live endpoint answers with.
	var cached struct {
		Name  string
		Units []hangarUnitView
		BV    int
	}
	unescaped := strings.NewReplacer("&#34;", `"`, "&amp;", "&", "&lt;", "<", "&gt;", ">", "&#39;", "'").Replace(attr)
	if err := json.Unmarshal([]byte(unescaped), &cached); err != nil || len(cached.Units) != 4 || cached.BV != 5645 {
		t.Fatalf("cached payload: %+v %v", cached, err)
	}

	// The Manage page's Mechs list embeds it too, and sits under the pilots (and their memorial) and above the records link.
	man := body(t, srv, "/refusalwar/manage/", cookie)
	mattr := cachedAttr(man)
	i, j, k := strings.Index(man, `id="pilots"`), strings.Index(man, `id="hangar-units"`), strings.Index(man, `id="records-open"`)
	if !strings.Contains(mattr, "Mist Lynx") || !strings.Contains(man, `id="hangar-units-list"`) || !(i >= 0 && i < j && j < k) {
		t.Errorf("the manage page should list the cached mechs under the pilots: attr %.120s, positions %d %d %d", mattr, i, j, k)
	}

	// A live read refreshes the copy (write-through).
	fake.setHangar(hangarOf("BMMadCat_H"), nil)
	if r := get(t, srv, "/api/campaign/force?campaign=refusalwar", cookie); r.StatusCode != http.StatusOK {
		t.Fatalf("live read: %d", r.StatusCode)
	}
	if attr := cachedAttr(body(t, srv, "/refusalwar/", cookie)); !strings.Contains(attr, "Timber Wolf") || strings.Contains(attr, "Mist Lynx") {
		t.Errorf("the live read should have replaced the cached copy: %.300s", attr)
	}
	// A failed read keeps the last copy, so the page still has something to show.
	fake.setHangar(HangarForce{}, errors.New("mekbay is down"))
	if r := get(t, srv, "/api/campaign/force?campaign=refusalwar", cookie); r.StatusCode != http.StatusBadGateway {
		t.Errorf("a failed read: %d", r.StatusCode)
	}
	if attr := cachedAttr(body(t, srv, "/refusalwar/", cookie)); !strings.Contains(attr, "Timber Wolf") {
		t.Errorf("a failed read must not lose the cached copy: %.200s", attr)
	}

	// The scenario page's deploy popup gets the cached hangar too, in its own shape (limit, operation).
	fake.setHangar(startingHangar(), nil)
	if r := get(t, srv, "/api/mekbay/hangar?campaign=refusalwar&scenario=scenario_001", cookie); r.StatusCode != http.StatusOK {
		t.Fatalf("deploy popup read: %d", r.StatusCode)
	}
	sattr := cachedAttr(body(t, srv, "/refusalwar/scenario_001/", cookie))
	if !strings.Contains(sattr, "Mist Lynx") || !strings.Contains(sattr, "&#34;limit&#34;:10000") || !strings.Contains(sattr, "&#34;hasOperation&#34;:false") || !strings.Contains(sattr, "&#34;canModify&#34;:false") {
		t.Errorf("the scenario page should carry the cached hangar for the popup: %.300s", sattr)
	}

	// Changing the hangar link makes the old copy meaningless: it is not shown for the new link.
	other := "https://mekbay.com/?instance=01a12f98-8a21-7d12-b43d-52bb70872376"
	if r := post(t, srv, "/api/campaign/start", url.Values{"campaign": {"refusalwar"}, "hangar": {other}}, cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("change link: %d", r.StatusCode)
	}
	if attr := cachedAttr(body(t, srv, "/refusalwar/", cookie)); attr != "" {
		t.Errorf("a changed hangar link must not show the old copy: %.200s", attr)
	}
	// Reading the new link caches it for the next visit.
	if r := get(t, srv, "/api/campaign/force?campaign=refusalwar", cookie); r.StatusCode != http.StatusOK {
		t.Fatal(r.StatusCode)
	}
	if attr := cachedAttr(body(t, srv, "/refusalwar/", cookie)); !strings.Contains(attr, "Mist Lynx") {
		t.Errorf("the new link's hangar should now be cached: %.200s", attr)
	}

	// Deleting the campaign takes its cached hangar with it.
	if r := post(t, srv, "/api/campaign/delete", url.Values{"campaign": {"refusalwar"}}, cookie, srv.URL); r.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", r.StatusCode)
	}
	if _, _, ok, _ := store.CachedHangar(ctx, id, "refusalwar", "01a12f98-8a21-7d12-b43d-52bb70872376"); ok {
		t.Error("deleting the campaign should clear its cached hangar")
	}
}

// Each hangar unit carries the link to its own page on mekbay.com (the generic sheet, not a unit in a force).
func TestUnitPageURL(t *testing.T) {
	for in, want := range map[string]string{
		"BMMadCat_H":     "https://mekbay.com/?gs=cbt&shareUnit=BMMadCat_H&tab=Sheet",
		"BMKoshi_E":      "https://mekbay.com/?gs=cbt&shareUnit=BMKoshi_E&tab=Sheet",
		"BMGrizzly":      "https://mekbay.com/?gs=cbt&shareUnit=BMGrizzly&tab=Sheet",
		"BM Odd&Name=1#": "https://mekbay.com/?gs=cbt&shareUnit=BM+Odd%26Name%3D1%23&tab=Sheet",
		"":               "",
	} {
		if got := unitPageURL(in); got != want {
			t.Errorf("unitPageURL(%q) = %q, want %q", in, got, want)
		}
	}
	views := hangarViews(hangarOf("BMMadCat_H", "BMNotAUnit"))
	if views[0].MekBayURL != "https://mekbay.com/?gs=cbt&shareUnit=BMMadCat_H&tab=Sheet" || views[1].MekBayURL == "" {
		t.Errorf("hangar views carry the unit's MekBay page: %+v", views)
	}
	b, err := json.Marshal(views[0])
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil || decoded["mekbayUrl"] != "https://mekbay.com/?gs=cbt&shareUnit=BMMadCat_H&tab=Sheet" {
		t.Errorf("the view's JSON should carry mekbayUrl: %v %v", decoded["mekbayUrl"], err)
	}
}
