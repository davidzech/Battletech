package site

import (
	"encoding/json"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// A link to a unit's page on mekbay.com in a campaign's Markdown (an OpFor line in Setup.md:
// `[Nova Prime](https://mekbay.com/?gs=cbt&shareUnit=BMBlackHawk_Prime&tab=General) (3/4) Skirmisher: ...`) names the unit by its MekBay ID, so
// the renderer looks it up in the embedded catalog and gives the link the unit's details; static/unitlinks.js shows them as a hover card. The
// pilot's skills written right after the link, "(3/4)", add the BV the unit has at those skills. A link to a unit the catalog doesn't know is left
// plain, and reported as a problem of the campaign (unknownUnitLinks).

// unitLinkHTML matches a rendered link to mekbay.com and the skills that may follow it. The link's attributes are the renderer's own (href, then
// target and rel added by renderMarkdown), never a visitor's.
var unitLinkHTML = regexp.MustCompile(`<a href="(https://(?:www\.)?mekbay\.com/[^"]*)"([^>]*)>((?s:.*?))</a>(\s*\(([0-8])/([0-8])\))?`)

// unitCard is what a unit's hover card shows (static/unitcard.js), on a scenario's unit links and on the overview's unit icons (hangarUnitView.Card).
type unitCard struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`  // the Clan name when the design has one
	Model  string  `json:"model"` // e.g. "Prime"
	Icon   string  `json:"icon"`
	Class  string  `json:"class"`
	Role   string  `json:"role"`
	Tons   float64 `json:"tons"`
	Tech   string  `json:"tech"`
	BV     int     `json:"bv"` // base BV
	Engine string  `json:"engine,omitempty"`
	Omni   bool    `json:"omni,omitempty"`
	// The mech's construction, the card's second column: armor and internal structure points and their types, heat generated and dissipated, and
	// Walk/Run/Jump movement points.
	Armor         int           `json:"armor,omitempty"`
	ArmorType     string        `json:"armorType,omitempty"`
	Structure     int           `json:"structure,omitempty"`
	StructureType string        `json:"structureType,omitempty"`
	Heat          int           `json:"heat,omitempty"`
	Dissipation   int           `json:"dissipation,omitempty"`
	Walk          int           `json:"walk,omitempty"`
	Run           int           `json:"run,omitempty"`
	Jump          int           `json:"jump"`
	Firepower     int           `json:"firepower,omitempty"` // every weapon fired at once
	MaxRange      int           `json:"maxRange,omitempty"`  // the longest weapon range, in hexes
	Range         int           `json:"range,omitempty"`     // the range weighted by each weapon's damage
	Weapons       *weaponCounts `json:"weapons,omitempty"`   // how many ballistic, energy and missile weapons it mounts (mechs only)
	Gunnery       int           `json:"gunnery,omitempty"`   // from "(3/4)" after the link; 0 when none was written
	Piloting      int           `json:"piloting,omitempty"`
	SkillBV       int           `json:"skillBV,omitempty"` // the BV at those skills
}

// weaponCounts is how many weapons of each type a mech mounts, for the card's weapon type bar.
type weaponCounts struct {
	Ballistic int `json:"ballistic"`
	Energy    int `json:"energy"`
	Missile   int `json:"missile"`
}

// catalogCard is the card of a catalog unit, without any pilot's skills.
func catalogCard(u Unit) *unitCard {
	name, model := unitNames(u)
	card := &unitCard{ID: u.Name, Name: name, Model: model, Icon: iconURL(u), Class: u.Class, Role: u.Role, Tons: u.Tons, Tech: u.TechBase,
		BV: u.BV, Engine: u.Engine, Omni: u.Omni == 1,
		Armor: u.Armor, ArmorType: strings.TrimSuffix(u.ArmorType, " Armor"), Structure: u.Internal, StructureType: u.StructureType,
		Heat: u.Heat, Dissipation: u.Dissipation, Walk: u.Walk, Run: u.Run, Jump: u.Jump,
		Firepower: u.Firepower, MaxRange: u.MaxRange, Range: u.Range}
	if u.Type == "Mek" {
		card.Weapons = &weaponCounts{u.Ballistic, u.Energy, u.Missile}
	}
	return card
}

// shareUnitID is the MekBay unit ID a mekbay.com link opens (its shareUnit parameter), "" for any other link.
func shareUnitID(href string) string {
	u, err := url.Parse(html.UnescapeString(href))
	if err != nil {
		return ""
	}
	return u.Query().Get("shareUnit")
}

// annotateUnitLinks gives every link to a known unit's MekBay page the class unit-link and its card as JSON in data-unit.
func annotateUnitLinks(out string) string {
	return unitLinkHTML.ReplaceAllStringFunc(out, func(m string) string {
		p := unitLinkHTML.FindStringSubmatch(m)
		id := shareUnitID(p[1])
		u, ok := unitByName(id)
		if id == "" || !ok {
			return m
		}
		card := catalogCard(u)
		if p[4] != "" {
			card.Gunnery, _ = strconv.Atoi(p[5])
			card.Piloting, _ = strconv.Atoi(p[6])
			card.SkillBV = adjustedBV(u.BV, card.Gunnery, card.Piloting)
		}
		data, err := json.Marshal(card)
		if err != nil {
			return m
		}
		return `<a href="` + p[1] + `"` + p[2] + ` class="unit-link" data-unit="` + html.EscapeString(string(data)) + `">` + p[3] + `</a>` + p[4]
	})
}

// unknownUnitLinks lists the MekBay unit IDs that rendered links name but the catalog doesn't know (a typo, or a unit added to MekBay since the
// catalog was refreshed).
func unknownUnitLinks(out string) []string {
	var ids []string
	for _, p := range unitLinkHTML.FindAllStringSubmatch(out, -1) {
		if strings.Contains(p[2], `class="unit-link"`) {
			continue
		}
		if id := shareUnitID(p[1]); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}
