package site

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/mekbay"
)

// A pull-through cache of each campaign run's MekBay hangar. Reading a hangar from MekBay takes a moment, and a page that has
// to wait for it shows an empty "reading your hangar" state. So every successful live read is also saved in the database
// (table hangar_cache, one row per playthrough and campaign), and the pages that show the hangar embed the last saved copy:
// they paint it straight away, then read MekBay again in the background and replace it with the fresh data. A run that has
// never been read shows the loading state, once. The copy belongs to the hangar link it was read from: change the link and the
// old copy is ignored.

// forceData is the hangar as the campaign overview and the Manage page show it: every unit, and the base BV they sum to (an unknown unit adds
// nothing; a destroyed one still counts, being still in the hangar), with each omnimech's variants marked as available to the campaign's faction
// in its era or not.
func forceData(h mekbay.HangarForce, c *content.Campaign) map[string]any {
	units := hangarViews(h)
	markSwapAvailability(units, c)
	bv := 0
	for _, u := range units {
		if !u.Unknown {
			bv += u.BV
		}
	}
	return map[string]any{"name": h.Name, "units": units, "bv": bv}
}

// deployData is the hangar (or the scenario's provided mechs) as the scenario's deploy popup shows it. `provided` says the mechs are the scenario's own, and
// `swaps` that the wizard has a Configure step where they can be reconfigured for free, to the variants each omnimech's swapOptions mark as available.
func deployData(h mekbay.HangarForce, c *content.Campaign, sc *content.Scenario, canModify, hasOperation bool) map[string]any {
	units := hangarViews(h)
	markSwapAvailability(units, c)
	return map[string]any{"name": h.Name, "limit": sc.BVLimit, "canModify": canModify, "units": units, "hasOperation": hasOperation, "provided": sc.Provides(), "swaps": sc.Provides() && sc.ProvidedSwaps}
}

func cachedJSON(data map[string]any, at time.Time) string {
	data["cachedAt"] = at.UTC().Format(time.RFC3339)
	b, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	return string(b)
}

// ForceJSON is the cached hangar for the campaign overview, as the JSON the overview script renders ("" if there is none yet).
func (p Page) ForceJSON() string {
	if p.CachedHangar == nil {
		return ""
	}
	return cachedJSON(forceData(*p.CachedHangar, p.Campaign), p.CachedAt)
}

// PilotsJSON is the campaign's living named pilots for the Deploy popup's pilot tray: who they are, their four numbers, and (for the hover card, which
// looks like the Manage page's pilot card) each column's level, next level and SP progress, the SP earned and the Edge abilities learned.
func (p Page) PilotsJSON() string {
	type column struct {
		Label   string `json:"label"`
		Level   int    `json:"level"`
		SP      int    `json:"sp"`
		HasNext bool   `json:"hasNext"`
		NextAt  int    `json:"nextAt"`
		NextLvl int    `json:"nextLvl"`
		Percent int    `json:"percent"`
	}
	type learned struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type tray struct {
		Name        string    `json:"name"`
		Rank        string    `json:"rank"`
		PortraitURL string    `json:"portraitUrl"`
		Gunnery     int       `json:"gunnery"`
		Piloting    int       `json:"piloting"`
		EdgeTokens  int       `json:"edgeTokens"`
		Abilities   int       `json:"abilities"` // how many Edge abilities their level lets them hold
		Earned      int       `json:"earned"`
		Columns     []column  `json:"columns"`
		Learned     []learned `json:"learned"` // the Edge abilities they have chosen
	}
	out := []tray{}
	for _, v := range p.Pilots {
		if v.Fallen {
			continue
		}
		t := tray{Name: v.Name, Rank: v.Rank, PortraitURL: v.PortraitURL, Gunnery: v.Gunnery, Piloting: v.Piloting, EdgeTokens: v.EdgeTokens, Abilities: v.Abilities, Earned: v.Earned, Columns: []column{}, Learned: []learned{}}
		for _, c := range v.Columns {
			t.Columns = append(t.Columns, column{c.Label, c.Level, c.SP, c.HasNext, c.NextAt, c.NextLvl, c.Percent})
		}
		for _, a := range v.EdgeLearned {
			t.Learned = append(t.Learned, learned{a.ID, a.Name})
		}
		out = append(out, t)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// DeployJSON is the cached hangar for the scenario's deploy popup ("" if there is none yet).
func (p Page) DeployJSON() string {
	if p.Scenario != nil && p.Scenario.Provides() {
		// The scenario's own mechs need no read from MekBay, so the popup can show them at once.
		return cachedJSON(deployData(providedForce(p.Scenario), p.Campaign, p.Scenario, p.Run.Owned(), p.InProgress), time.Now())
	}
	if p.CachedHangar == nil || p.Scenario == nil {
		return ""
	}
	return cachedJSON(deployData(*p.CachedHangar, p.Campaign, p.Scenario, p.Run.Owned(), p.InProgress), p.CachedAt)
}

// cacheHangar saves a hangar that was just read live. A failure only costs the cache, so it is logged and otherwise ignored.
func (s *Server) cacheHangar(ctx context.Context, playthroughID, campaign, instance string, h mekbay.HangarForce) {
	if err := s.store.SaveHangarCache(ctx, playthroughID, campaign, instance, h); err != nil {
		log.Printf("cache hangar: %v", err)
	}
}
