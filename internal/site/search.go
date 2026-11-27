package site

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/davidzech/Battletech/internal/catalog"
)

// The unit search endpoint: the search itself and the faction and era filter are package catalog (search.go, availability.go).

// searchResult is what a search returns: the units shown, how many matched in all, and what filter was applied.
type searchResult struct {
	Units   []hangarUnitView `json:"units"`
	Total   int              `json:"total"`
	Filter  searchFilter     `json:"filter"`
	Limited bool             `json:"limited"` // more matched than are shown
}

type searchFilter struct {
	Faction string `json:"faction"`
	Era     string `json:"era"`
	Active  bool   `json:"active"` // only units available to the faction in the era are listed
}

// catalogUnitView shows a catalog unit as the same view the hangar uses, plus the MekBay unit ID the purchase needs.
func catalogUnitView(u catalog.Unit) hangarUnitView {
	v := hangarUnitView{MekBayURL: catalog.PageURL(u.Name), Unit: u.Name}
	v.Name, v.Model = catalog.Names(u)
	v.Class, v.Role, v.Tons, v.Icon, v.BV = u.Class, catalog.RoleLabel(u), u.Tons, catalog.IconURL(u), u.BV
	return v
}

// searchUnitsHandler is GET /api/units/search?q=&campaign=&all=1. The list is public MekBay data, so it needs no seed. With a campaign that names a
// faction and an era the results are limited to what that faction fields in that era, unless all=1.
func (s *Server) searchUnitsHandler(w http.ResponseWriter, r *http.Request) {
	res := searchResult{Units: []hangarUnitView{}}
	var available map[string]bool
	if c, ok := s.lib.Campaign(r.URL.Query().Get("campaign")); ok && c.AvailabilityFaction != "" && c.AvailabilityEra != "" {
		res.Filter = searchFilter{Faction: c.AvailabilityFaction, Era: c.AvailabilityEra}
		if available = catalog.Available(c.AvailabilityFaction, c.AvailabilityEra); available != nil && r.URL.Query().Get("all") != "1" {
			res.Filter.Active = true
		} else {
			available = nil
		}
	}
	found, total := catalog.Search(strings.TrimSpace(r.URL.Query().Get("q")), available)
	for _, u := range found {
		res.Units = append(res.Units, catalogUnitView(u))
	}
	res.Total, res.Limited = total, total > len(found)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(res)
}

// unitsByIDHandler is GET /api/units?ids=A,B: the units with those MekBay unit IDs, as the search shows them, in the order asked (an unknown ID is left out). The
// Purchase dialog uses it to put a cart back from its URL. Like the search it is public data and needs no seed.
func (s *Server) unitsByIDHandler(w http.ResponseWriter, r *http.Request) {
	out := struct {
		Units []hangarUnitView `json:"units"`
	}{Units: []hangarUnitView{}}
	ids := strings.Split(r.URL.Query().Get("ids"), ",")
	for i, id := range ids {
		if i == maxUnitLookup {
			break
		}
		if u, ok := catalog.ByName(strings.TrimSpace(id)); ok && u.Type == "Mek" {
			out.Units = append(out.Units, catalogUnitView(u))
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}

// maxUnitLookup bounds GET /api/units: a cart holds at most ten units.
const maxUnitLookup = 20
