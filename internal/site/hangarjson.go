package site

import (
	"encoding/json"

	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/storage"
)

// The hangar as the pages' scripts take it: the campaign overview, the Manage page and the Deploy popup embed it in the page (it is in the database, so the page
// has it at once), and GET /api/campaign/force and GET /api/mekbay/hangar answer the same JSON.

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
func deployData(h mekbay.HangarForce, c *content.Campaign, sc *content.Scenario, run storage.CampaignRun) map[string]any {
	units := hangarViews(h)
	markSwapAvailability(units, c)
	// The review offers to reconfigure omnimechs: a scenario's own mechs for free when it allows it, a hangar's at the Manage page's price (paid by Start
	// battle, from the warchest).
	swaps, paid := sc.Provides() && sc.ProvidedSwaps, false
	if !sc.Provides() {
		swaps, paid = true, true
	}
	return map[string]any{"name": h.Name, "limit": sc.BVLimit, "units": units, "provided": sc.Provides(), "swaps": swaps, "paidSwaps": paid, "warchest": run.Warchest}
}

func pageJSON(data map[string]any) string {
	b, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	return string(b)
}

// ForceJSON is the hangar for the campaign overview and the Manage page, as the JSON their scripts render ("" if it could not be loaded).
func (p Page) ForceJSON() string {
	if p.Hangar == nil {
		return ""
	}
	return pageJSON(forceData(*p.Hangar, p.Campaign))
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

// DeployJSON is the hangar (or the scenario's own mechs) for the scenario's deploy popup ("" if it could not be loaded).
func (p Page) DeployJSON() string {
	if p.Scenario != nil && p.Scenario.Provides() {
		return pageJSON(deployData(providedForce(p.Scenario), p.Campaign, p.Scenario, p.Run))
	}
	if p.Hangar == nil || p.Scenario == nil {
		return ""
	}
	return pageJSON(deployData(*p.Hangar, p.Campaign, p.Scenario, p.Run))
}
