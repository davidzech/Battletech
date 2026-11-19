package site

import (
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"testing"
)

func TestUnitLinksCarryTheirCards(t *testing.T) {
	out := string(renderMarkdown("- [Nova Prime](https://mekbay.com/?gs=cbt&shareUnit=BMBlackHawk_Prime&tab=General) (3/4) Skirmisher: Alrek\n"+
		"- [Grizzly](https://mekbay.com/?gs=cbt&shareUnit=BMGrizzly&tab=General) Brawler\n"+
		"- [Nothing](https://mekbay.com/?gs=cbt&shareUnit=BMNoSuchMech_X&tab=General) (3/4)\n"+
		"- [Elsewhere](https://www.sarna.net/wiki/Nova)\n", false))

	cards := regexp.MustCompile(`class="unit-link" data-unit="([^"]*)"`).FindAllStringSubmatch(out, -1)
	if len(cards) != 2 {
		t.Fatalf("the two known units get cards, the unknown one and the other site don't: %d in %s", len(cards), out)
	}
	var nova, grizzly unitLinkCard
	if err := json.Unmarshal([]byte(html.UnescapeString(cards[0][1])), &nova); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(html.UnescapeString(cards[1][1])), &grizzly); err != nil {
		t.Fatal(err)
	}
	if nova.ID != "BMBlackHawk_Prime" || nova.Name != "Nova" || nova.Model != "Prime" || nova.BV <= 0 || nova.Tons != 50 || !strings.HasPrefix(nova.Icon, "/icons/") {
		t.Errorf("the card is the catalog's unit, by its Clan name: %+v", nova)
	}
	if nova.Gunnery != 3 || nova.Piloting != 4 || nova.SkillBV != adjustedBV(nova.BV, 3, 4) || nova.SkillBV <= nova.BV {
		t.Errorf("the skills after the link give the BV at those skills: %+v", nova)
	}
	// The Nova Prime's construction, as the catalog has it (the type without its trailing " Armor").
	if nova.Armor != 160 || nova.ArmorType != "Standard" || nova.Structure != 83 || nova.StructureType != "Standard" || nova.Heat != 60 || nova.Dissipation != 36 ||
		nova.Walk != 5 || nova.Run != 8 || nova.Jump != 5 || nova.Firepower != 84 || nova.MaxRange != 15 || nova.Range != 15 {
		t.Errorf("the card carries armor, structure, heat and movement: %+v", nova)
	}
	if grizzly.Gunnery != 0 || grizzly.SkillBV != 0 {
		t.Errorf("no skills written, none on the card: %+v", grizzly)
	}
	// The link still opens MekBay in a new tab, and the skills stay in the text.
	if !strings.Contains(out, `href="https://mekbay.com/?gs=cbt&amp;shareUnit=BMBlackHawk_Prime&amp;tab=General" target="_blank" rel="noopener noreferrer" class="unit-link"`) || !strings.Contains(out, "</a> (3/4) Skirmisher") {
		t.Errorf("the link is otherwise unchanged: %s", out)
	}
	if got := unknownUnitLinks(out); len(got) != 1 || got[0] != "BMNoSuchMech_X" {
		t.Errorf("the unknown unit is reported: %v", got)
	}
}

func TestScenarioPagesHaveUnitCards(t *testing.T) {
	c, ok := Library{Root: "../../campaigns"}.Campaign("refusalwar")
	if !ok {
		t.Fatal("no Refusal War campaign")
	}
	for _, sc := range c.Scenarios {
		if len(sc.Forces) > 0 && !strings.Contains(string(sc.SetupHTML), `class="unit-link"`) {
			t.Errorf("%s: its OpFor links have no hover cards", sc.Slug)
		}
	}
	// And the page loads the script, after mechrow.js, whose floating card it uses.
	page, err := assets.ReadFile("templates/scenario.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(page)
	if i, j := strings.Index(s, "/static/mechrow.js"), strings.Index(s, "/static/unitlinks.js"); i < 0 || j < i || strings.Count(s, "/static/mechrow.js") != 1 {
		t.Error("scenario.html loads mechrow.js once, then unitlinks.js")
	}
}

// Firepower and range follow MekBay's stat bars: the Atlas AS7-D's AC/20, LRM 20, SRM 6 and four medium lasers fire 20 + 20 + 12 + 4x5 = 72, the LRM
// reaches 21 hexes, and the damage-weighted range is 9.
func TestFirepowerAndRange(t *testing.T) {
	u, ok := unitByName("BMAtlas_AS7D")
	if !ok {
		t.Fatal("no Atlas AS7-D in the catalog")
	}
	if u.Firepower != 72 || u.MaxRange != 21 || u.Range != 9 {
		t.Errorf("Atlas AS7-D: firepower %d, range %d (%d weighted), want 72, 21 (9)", u.Firepower, u.MaxRange, u.Range)
	}
}
