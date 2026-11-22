package catalog

import (
	"encoding/json"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// unitLinkHTML matches a rendered link to mekbay.com and the skills that may follow it. The link's attributes are the renderer's own (href, then
// target and rel added by renderMarkdown), never a visitor's.
var unitLinkHTML = regexp.MustCompile(`<a href="(https://(?:www\.)?mekbay\.com/[^"]*)"([^>]*)>((?s:.*?))</a>(\s*\(([0-8])/([0-8])\))?`)

// shareUnitID is the MekBay unit ID a mekbay.com link opens (its shareUnit parameter), "" for any other link.
func shareUnitID(href string) string {
	u, err := url.Parse(html.UnescapeString(href))
	if err != nil {
		return ""
	}
	return u.Query().Get("shareUnit")
}

// AnnotateUnitLinks gives every link to a known unit's MekBay page the class unit-link and its card as JSON in data-unit.
func AnnotateUnitLinks(out string) string {
	return unitLinkHTML.ReplaceAllStringFunc(out, func(m string) string {
		p := unitLinkHTML.FindStringSubmatch(m)
		id := shareUnitID(p[1])
		u, ok := ByName(id)
		if id == "" || !ok {
			return m
		}
		card := CardFor(u)
		if p[4] != "" {
			card.Gunnery, _ = strconv.Atoi(p[5])
			card.Piloting, _ = strconv.Atoi(p[6])
			card.SkillBV = AdjustedBV(u.BV, card.Gunnery, card.Piloting)
		}
		data, err := json.Marshal(card)
		if err != nil {
			return m
		}
		return `<a href="` + p[1] + `"` + p[2] + ` class="unit-link" data-unit="` + html.EscapeString(string(data)) + `">` + p[3] + `</a>` + p[4]
	})
}

// UnknownUnitLinks lists the MekBay unit IDs that rendered links name but the catalog doesn't know (a typo, or a unit added to MekBay since the
// catalog was refreshed).
func UnknownUnitLinks(out string) []string {
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
