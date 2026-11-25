package site

import (
	"log"
	"net/http"
	"slices"
	"strings"

	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/storage"
)

// KeywordView is a keyword the campaign has earned, as the overview shows it: its name, its description from
// Keywords.md (the chip's hover card) and the mission it comes from. Players never see the keyword's ID.
type KeywordView struct {
	Name string
	Text string
	From string // "Scenario 001: Pyre Light", or "" when that scenario isn't known
}

// keywordViews reads the campaign's ledger for the overview ("" on a failure, which is logged).
func (s *Server) keywordViews(r *http.Request, playthroughID string, c *content.Campaign) []KeywordView {
	ledger, err := s.store.Ledger(r.Context(), playthroughID, c.Slug)
	if err != nil {
		log.Printf("load the campaign ledger: %v", err)
		return nil
	}
	out := make([]KeywordView, 0, len(ledger))
	for _, e := range ledger {
		// A registered keyword comes from its own scenario, even one ticked by hand in Edit keywords; one the
		// registry no longer has falls back to whatever the ledger recorded.
		k, ok := c.Keyword(e.Keyword)
		source := e.Scenario
		if ok {
			source = k.Scenario
		}
		v := KeywordView{Name: c.KeywordName(e.Keyword), Text: k.Description}
		if sc, ok := c.Scenario(source); ok {
			v.From = c.MissionName(*sc)
		}
		out = append(out, v)
	}
	return out
}

// KeywordChoice is one of the campaign's registered keywords in the Manage page's Edit keywords dialog, ticked
// when the campaign holds it.
type KeywordChoice struct {
	ID   string
	Name string
	Text string
	Held bool
}

// keywordChoices lists every keyword Keywords.md registers, in its order, marking the ones the ledger holds.
func (s *Server) keywordChoices(r *http.Request, playthroughID string, c *content.Campaign) []KeywordChoice {
	ledger, err := s.store.Ledger(r.Context(), playthroughID, c.Slug)
	if err != nil {
		log.Printf("load the campaign ledger: %v", err)
	}
	held := map[string]bool{}
	for _, e := range ledger {
		held[e.Keyword] = true
	}
	out := make([]KeywordChoice, 0, len(c.Keywords))
	for _, k := range c.Keywords {
		out = append(out, KeywordChoice{ID: k.ID, Name: k.Name, Text: k.Description, Held: held[k.ID]})
	}
	return out
}

// setKeywords is POST /api/campaign/keywords (repeated keyword fields): the campaign's ledger becomes exactly those
// keywords, each of which must be registered in Keywords.md. A correction from the Danger zone.
func (s *Server) setKeywords(w http.ResponseWriter, r *http.Request) {
	pt, c, _, ok := s.runRequest(w, r)
	if !ok {
		return
	}
	var keywords []string
	for _, k := range r.PostForm["keyword"] {
		if _, ok := c.Keyword(k); !ok {
			http.Error(w, "Unknown keyword: "+k, http.StatusBadRequest)
			return
		}
		if !slices.Contains(keywords, k) {
			keywords = append(keywords, k)
		}
	}
	if err := s.store.SetKeywords(r.Context(), pt.ID, c.Slug, keywords); err != nil {
		log.Printf("set keywords: %v", err)
		http.Error(w, "could not save the keywords", http.StatusInternalServerError)
		return
	}
	names := make([]string, 0, len(keywords))
	for _, k := range keywords {
		names = append(names, c.KeywordName(k))
	}
	text := "The keywords were edited: none held."
	if len(names) > 0 {
		text = "The keywords were edited: " + strings.Join(names, ", ") + "."
	}
	s.logHistory(r, pt.ID, c.Slug, storage.HistoryKeywords, 0, text)
	w.WriteHeader(http.StatusNoContent)
}
