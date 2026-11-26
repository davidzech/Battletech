package site

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/format"
	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/pilot"
	"github.com/davidzech/Battletech/internal/storage"
)

// The campaign history (storage/history.go) is written by every endpoint that changes a started campaign, once the change has been made: a line of text in the
// players' terms (names, not IDs) and the change to the warchest. Writing it is best effort: the change has already happened, so a failure is only logged.

// logHistory appends a line to the campaign's history. It runs detached from the request, so a browser that goes away doesn't lose the line.
func (s *Server) logHistory(r *http.Request, playthroughID, campaign, kind string, sp int, text string) {
	if err := s.store.LogHistory(context.WithoutCancel(r.Context()), playthroughID, campaign, kind, text, sp); err != nil {
		log.Printf("log the campaign history (%s): %v", text, err)
	}
}

// pilotByID finds a pilot of the campaign by ID, for the history to name them; ok is false when there is none (or the pilots can't be read).
func (s *Server) pilotByID(r *http.Request, playthroughID, campaign, id string) (p pilot.NamedPilot, ok bool) {
	pilots, err := s.store.Pilots(r.Context(), playthroughID, campaign)
	if err != nil {
		return p, false
	}
	for _, p := range pilots {
		if p.ID == id {
			return p, true
		}
	}
	return p, false
}

// pilotLabel names a pilot in the history: their name, or "A pilot" when they couldn't be found.
func pilotLabel(p pilot.NamedPilot, ok bool) string {
	if !ok || p.Name == "" {
		return "A pilot"
	}
	return p.Name
}

// allocationText describes SP spent across the four columns, e.g. "Gunnery +40, Piloting +20".
func allocationText(a pilot.SPAllocation) string {
	var parts []string
	for _, c := range []struct {
		name string
		n    int
	}{{"Gunnery", a.Gunnery}, {"Piloting", a.Piloting}, {"Edge tokens", a.EdgeTokens}, {"Edge abilities", a.EdgeAbilities}} {
		if c.n > 0 {
			parts = append(parts, fmt.Sprintf("%s +%d", c.name, c.n))
		}
	}
	return strings.Join(parts, ", ")
}

// abilityNames lists Edge abilities by name.
func abilityNames(ids []string) string {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if a, ok := pilot.AbilityByID(id); ok {
			names = append(names, a.Name)
		} else {
			names = append(names, id)
		}
	}
	return strings.Join(names, ", ")
}

// repairLevelText names a repair level in the history.
var repairLevelText = map[string]string{
	mekbay.RepairArmor:     "armor",
	mekbay.RepairStructure: "structure and critical hits",
	mekbay.RepairCrippled:  "crippled",
	mekbay.RepairDestroyed: "destroyed",
}

// historyView is one line of the History dialog.
type historyView struct {
	When  string // e.g. "25 Nov 2026, 14:03"
	Kind  string
	Label string // the kind as the dialog shows it
	Text  string
	SP    string // the change to the warchest, e.g. "+360 SP" or "−300 SP"; "" for none
	Gain  bool
	// Checkpoint is the rollback point taken right after this line (its seq), which the row offers to roll back to; 0 for a line with none. CheckpointLabel
	// is its label, for the confirmation.
	Checkpoint      int
	CheckpointLabel string
}

// startCheckpointLabel is the label of the checkpoint taken as a campaign starts.
const startCheckpointLabel = "Campaign started"

var historyLabels = map[string]string{
	storage.HistoryCampaign:    "Campaign",
	storage.HistoryMission:     "Mission",
	storage.HistoryPilot:       "Pilot",
	storage.HistoryHangar:      "Hangar",
	storage.HistoryCorrections: "Corrections",
	storage.HistoryKeywords:    "Keywords",
}

// historyFilter is one of the History dialog's filter buttons: every kind has one, disabled when the history holds none of it.
type historyFilter struct {
	Kind, Label string
	Disabled    bool
}

// historyOrder is the order of the filter buttons.
var historyOrder = []string{storage.HistoryMission, storage.HistoryPilot, storage.HistoryHangar, storage.HistoryCampaign, storage.HistoryKeywords, storage.HistoryCorrections}

// portraitHistorySuffix ends the line logged when a pilot's portrait changes. Such lines are kept in the database but left out of
// the History dialog, where they are noise.
const portraitHistorySuffix = " changed portrait."

// shownInHistory says whether the History dialog shows an entry.
func shownInHistory(e storage.HistoryEntry) bool {
	return !(e.Kind == storage.HistoryPilot && strings.HasSuffix(e.Text, portraitHistorySuffix))
}

// historyViews reads a campaign's history for the History dialog, newest first (without the lines shownInHistory hides), and its filters (one per kind, disabled for a kind it doesn't hold).
func (s *Server) historyViews(r *http.Request, playthroughID string, c *content.Campaign) ([]historyView, []historyFilter) {
	entries, err := s.store.History(r.Context(), playthroughID, c.Slug)
	if err != nil {
		log.Printf("read the campaign history: %v", err)
		return nil, nil
	}
	shown := entries[:0:0]
	for _, e := range entries {
		if shownInHistory(e) {
			shown = append(shown, e)
		}
	}
	entries = shown
	held := map[string]bool{}
	for _, e := range entries {
		held[e.Kind] = true
	}
	// The lines a checkpoint was taken right after (the start, each completed mission) can be rolled back to. A checkpoint older than the history (history_seq 0)
	// belongs to the backfilled start line only if it is the start's own. A checkpoint's own last line may be one the dialog hides (a portrait changed while the
	// hangar was being frozen), so it is offered on the newest line shown at or before it.
	atLine := map[int]storage.Checkpoint{}
	if cps, err := s.store.Checkpoints(r.Context(), playthroughID, c.Slug); err != nil {
		log.Printf("read the checkpoints for the history: %v", err)
	} else {
		for _, cp := range cps {
			if cp.HistorySeq == 0 && cp.Label != startCheckpointLabel {
				continue
			}
			n := sort.Search(len(entries), func(i int) bool { return entries[i].Seq > cp.HistorySeq }) // entries are oldest first
			if n == 0 {
				continue
			}
			line := entries[n-1].Seq
			if _, taken := atLine[line]; !taken { // newest first: the latest checkpoint at a line wins
				atLine[line] = cp
			}
		}
	}
	filters := make([]historyFilter, 0, len(historyOrder))
	for _, k := range historyOrder {
		filters = append(filters, historyFilter{k, historyLabels[k], !held[k]})
	}
	out := make([]historyView, 0, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		v := historyView{When: e.At.Format("2 Jan 2006, 15:04"), Kind: e.Kind, Label: historyLabels[e.Kind], Text: e.Text}
		if cp, ok := atLine[e.Seq]; ok {
			v.Checkpoint, v.CheckpointLabel = cp.Seq, cp.Label
		}
		if v.Label == "" {
			v.Label = e.Kind
		}
		switch {
		case e.SP > 0:
			v.SP, v.Gain = "+"+format.Commas(e.SP)+" SP", true
		case e.SP < 0:
			v.SP = "−" + format.Commas(-e.SP) + " SP"
		}
		out = append(out, v)
	}
	return out, filters
}

// battleHistory describes a battle as it starts: who took which mechs, flown by whom, and the force's BV.
func battleHistory(bp *builtPlan) string {
	var players []string
	for _, p := range bp.res.Players {
		pilotOf := map[string]string{}
		for _, c := range p.Crew {
			pilotOf[c.Unit] = c.Pilot
		}
		mechs := make([]string, 0, len(p.Units))
		for _, u := range p.Units {
			m := strings.TrimSpace(u.Name + " " + u.Model)
			if who := pilotOf[u.ID]; who != "" {
				m += " (" + who + ")"
			}
			mechs = append(mechs, m)
		}
		line := strings.Join(mechs, ", ")
		if len(bp.res.Players) > 1 {
			line = fmt.Sprintf("Player %d: %s", p.Player, line)
		}
		players = append(players, line)
	}
	text := fmt.Sprintf("Deployed for %s: %s; %s BV", bp.c.MissionName(*bp.sc), strings.Join(players, "; "), format.Commas(bp.res.TotalBV))
	if bp.res.Commander != "" {
		text += ", commanded by " + bp.res.Commander
	}
	return text + "."
}

// outcomeHistory describes a committed mission outcome: the pool, what each pilot earned, deaths, promotions, keywords and free recruits.
func outcomeHistory(c *content.Campaign, sc *content.Scenario, res content.OutcomeResult) string {
	parts := []string{"Completed " + c.MissionName(*sc)}
	if res.Pool > 0 {
		parts = append(parts, fmt.Sprintf("objectives awarded %s SP", format.Commas(res.Pool)))
	}
	var earned, lost, promoted []string
	for _, p := range res.Pilots {
		if p.SP > 0 {
			e := fmt.Sprintf("%s +%s SP", p.Name, format.Commas(p.SP))
			if p.MVP {
				e += " (MVP)"
			}
			earned = append(earned, e)
		}
		if p.KIA {
			lost = append(lost, p.Name)
		}
		if p.Rank != "" {
			promoted = append(promoted, p.Name+" to "+p.Rank)
		}
	}
	if len(earned) > 0 {
		parts = append(parts, strings.Join(earned, ", "))
	}
	if len(lost) > 0 {
		parts = append(parts, "lost "+strings.Join(lost, ", "))
	}
	if len(promoted) > 0 {
		parts = append(parts, "promoted "+strings.Join(promoted, ", "))
	}
	if len(res.Keywords) > 0 {
		names := make([]string, 0, len(res.Keywords))
		for _, k := range res.Keywords {
			names = append(names, c.KeywordName(k))
		}
		parts = append(parts, "keywords "+strings.Join(names, ", "))
	}
	if res.FreeRecruits > 0 {
		parts = append(parts, fmt.Sprintf("%d free recruit%s", res.FreeRecruits, map[bool]string{true: "", false: "s"}[res.FreeRecruits == 1]))
	}
	if len(parts) == 1 {
		return parts[0] + "."
	}
	return parts[0] + ": " + strings.Join(parts[1:], "; ") + "."
}
