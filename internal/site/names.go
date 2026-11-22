package site

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/davidzech/Battletech/internal/pilot"
)

// NameIdeas are a campaign's own example names (its Names.md front matter), offered as the placeholders of the force and pilot name boxes so the suggestions
// fit the campaign's faction and era. Each list that is given replaces the site's baseline list of the same kind (static/pilotnames.js); a list left out
// keeps the baseline. Pilot names follow the portrait's group: Male and Female portraits draw from their own list plus Neutral, any other from all three.
type NameIdeas struct {
	Forces  []string `yaml:"force_names" json:"forces,omitempty"`
	Male    []string `yaml:"male_pilot_names" json:"male,omitempty"`
	Female  []string `yaml:"female_pilot_names" json:"female,omitempty"`
	Neutral []string `yaml:"neutral_pilot_names" json:"neutral,omitempty"`
}

// loadNames reads dir/Names.md (optional) into the campaign and reports any name that a player couldn't enter themselves, since a suggestion must be a
// valid name.
func (c *Campaign) loadNames(dir string) {
	var n NameIdeas
	if _, err := readMarkdown(filepath.Join(dir, "Names.md"), &n); err != nil {
		c.Problems = append(c.Problems, "Names.md: "+err.Error())
		return
	}
	check := func(list []string, key string, parse func(string) (string, error)) {
		for _, name := range list {
			if clean, err := parse(name); err != nil || clean != name {
				c.Problems = append(c.Problems, fmt.Sprintf("Names.md: %s has %q, which is not a valid name", key, name))
			}
		}
	}
	check(n.Forces, "force_names", parseForceName)
	check(n.Male, "male_pilot_names", pilot.ParsePilotName)
	check(n.Female, "female_pilot_names", pilot.ParsePilotName)
	check(n.Neutral, "neutral_pilot_names", pilot.ParsePilotName)
	c.Names = n
}

// NamesJSON is the campaign's name ideas for static/pilotnames.js (its script tag's data-names): only the lists the campaign gives, "{}" when none.
func (c *Campaign) NamesJSON() string {
	b, err := json.Marshal(c.Names)
	if err != nil {
		return "{}"
	}
	return string(b)
}
