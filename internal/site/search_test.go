package site

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/catalog"
)

func TestSearchEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)
	get := func(q string) searchResult {
		t.Helper()
		res, err := http.Get(srv.URL + "/api/units/search?" + q)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
			t.Fatalf("%s: %d %s", q, res.StatusCode, res.Header.Get("Content-Type"))
		}
		var out searchResult
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("%s: %v: %s", q, err, b)
		}
		return out
	}
	// Refusal War names Clan Wolf in the Clan Invasion era: the default search is limited to it.
	r := get("campaign=refusalwar&q=timber+wolf")
	if !r.Filter.Active || r.Filter.Faction != "Clan Wolf" || r.Filter.Era != "Clan Invasion" || len(r.Units) == 0 {
		t.Fatalf("filtered search: %+v", r.Filter)
	}
	u := r.Units[0]
	if u.Unit == "" || u.Name == "" || u.Icon == "" || u.Role == "" || u.Tons == 0 || u.BV == 0 || u.MekBayURL == "" {
		t.Errorf("a result should carry everything a row and a purchase need: %+v", u)
	}
	// A mech Clan Wolf doesn't field: not offered under the filter, offered with all=1 (and the filter reports as off).
	um, uq := unavailableMech(t)
	enc := strings.ReplaceAll(uq, " ", "+")
	has := func(r searchResult) bool {
		for _, v := range r.Units {
			if v.Unit == um.Name {
				return true
			}
		}
		return false
	}
	if r := get("campaign=refusalwar&q=" + enc); has(r) || !r.Filter.Active {
		t.Errorf("%s under the filter: %+v", um.Name, r.Filter)
	}
	if r := get("campaign=refusalwar&all=1&q=" + enc); !has(r) || r.Filter.Active || r.Filter.Faction != "Clan Wolf" {
		t.Errorf("%s with all=1: %+v", um.Name, r.Filter)
	}
	// Without a campaign, or with one that names no faction, there is no filter; an empty query lists nothing.
	if r := get("q=" + enc); !has(r) || r.Filter.Active {
		t.Errorf("no campaign: %+v", r.Filter)
	}
	if r := get("campaign=refusalwar&q="); len(r.Units) != 0 || r.Units == nil {
		t.Errorf("empty query: %+v", r.Units)
	}
}

// unavailableMech is an Inner Sphere mech Clan Wolf doesn't field in the Clan Invasion era, and the text that finds it.
func unavailableMech(t *testing.T) (catalog.Unit, string) {
	t.Helper()
	wolf := catalog.Available("Clan Wolf", "Clan Invasion")
	for _, u := range catalog.All() {
		if u.Type == "Mek" && u.TechBase == "Inner Sphere" && !wolf[u.Name] && u.Model != "" {
			return u, u.Chassis + " " + u.Model
		}
	}
	t.Fatal("no unavailable mech found")
	return catalog.Unit{}, ""
}
