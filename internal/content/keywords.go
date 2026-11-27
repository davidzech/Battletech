package content

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Keyword is one entry of a campaign's keyword registry, the front matter of its Keywords.md:
//
//	keywords:
//	  - id: scorched_earth          # what scenarios grant and require; players never see it
//	    name: Scorched Earth        # what players see
//	    scenario: scenario_001      # the one scenario that grants it
//	    description: The chemical plant was completely destroyed.
//
// The Markdown body below the front matter is notes for authors and is not shown anywhere.
type Keyword struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	Scenario    string `yaml:"scenario"`
	Description string `yaml:"description"`
}

// keywordsMeta is the front matter of Keywords.md.
type keywordsMeta struct {
	Keywords []Keyword `yaml:"keywords"`
}

// loadKeywords reads the registry from Keywords.md. An entry with a bad or repeated ID, or no name or scenario, is a
// campaign problem and is left out. Its scenario may be one not written yet; checkGrants holds the written ones to it.
func (c *Campaign) loadKeywords(dir string) {
	var meta keywordsMeta
	if _, err := readMarkdown(filepath.Join(dir, "Keywords.md"), &meta); err != nil {
		c.Problems = append(c.Problems, "Keywords.md: "+err.Error())
		return
	}
	bad := func(format string, args ...any) {
		c.Problems = append(c.Problems, "Keywords.md: "+fmt.Sprintf(format, args...))
	}
	seen := map[string]bool{}
	for i, k := range meta.Keywords {
		k.ID, k.Name, k.Description = strings.TrimSpace(k.ID), strings.TrimSpace(k.Name), strings.TrimSpace(k.Description)
		k.Scenario = strings.ToLower(strings.TrimSpace(k.Scenario))
		switch {
		case !keywordID.MatchString(k.ID):
			bad("keyword %d has the id %q, which is not lowercase snake_case", i+1, k.ID)
		case seen[k.ID]:
			bad("the keyword %q is listed twice", k.ID)
		case k.Name == "":
			bad("the keyword %q has no name", k.ID)
		case !scenarioDir.MatchString(k.Scenario):
			bad("the keyword %q has the scenario %q, which is not a scenario ID like scenario_001", k.ID, k.Scenario)
		default:
			seen[k.ID] = true
			c.Keywords = append(c.Keywords, k)
		}
	}
}

// Keyword looks a registered keyword up by its ID.
func (c *Campaign) Keyword(id string) (Keyword, bool) {
	for _, k := range c.Keywords {
		if k.ID == id {
			return k, true
		}
	}
	return Keyword{}, false
}

// KeywordName is how a keyword is shown to players: its registered name, or else its ID in words ("scorched_earth"
// -> "Scorched Earth", for a keyword a ledger still holds after it left Keywords.md).
func (c *Campaign) KeywordName(id string) string {
	if k, ok := c.Keyword(id); ok {
		return k.Name
	}
	words := strings.Fields(strings.ReplaceAll(id, "_", " "))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// KeywordText is a keyword's description from Keywords.md ("" for one the registry doesn't have), for a keyword chip's hover card.
func (c *Campaign) KeywordText(id string) string {
	k, _ := c.Keyword(id)
	return k.Description
}

// KeywordNames is KeywordName for each of a list of keyword IDs.
func (c *Campaign) KeywordNames(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = c.KeywordName(id)
	}
	return out
}

// checkGrants marks a scenario whose outcome grants a keyword that Keywords.md doesn't register, or one registered as
// another scenario's: it then has no outcome engine, and the mistake is reported.
func (c *Campaign) checkGrants() {
	for i := range c.Scenarios {
		sc := &c.Scenarios[i]
		if sc.OutcomeError != "" {
			continue
		}
		for _, g := range sc.GrantedKeywords() {
			k, ok := c.Keyword(g)
			if !ok {
				sc.OutcomeError = fmt.Sprintf("grants the keyword %q, which is not in Keywords.md", g)
				break
			}
			if k.Scenario != sc.Slug {
				sc.OutcomeError = fmt.Sprintf("grants the keyword %q, which Keywords.md gives to %s", g, k.Scenario)
				break
			}
		}
	}
}
