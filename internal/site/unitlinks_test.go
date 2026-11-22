package site

import (
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/mekbay"
)

func TestUnitLinksCarryTheirCards(t *testing.T) {
	out := string(content.RenderMarkdown("- [Nova Prime](https://mekbay.com/?gs=cbt&shareUnit=BMBlackHawk_Prime&tab=General) (3/4) Skirmisher: Alrek\n"+
		"- [Grizzly](https://mekbay.com/?gs=cbt&shareUnit=BMGrizzly&tab=General) Brawler\n"+
		"- [Nothing](https://mekbay.com/?gs=cbt&shareUnit=BMNoSuchMech_X&tab=General) (3/4)\n"+
		"- [Elsewhere](https://www.sarna.net/wiki/Nova)\n", false))

	cards := regexp.MustCompile(`class="unit-link" data-unit="([^"]*)"`).FindAllStringSubmatch(out, -1)
	if len(cards) != 2 {
		t.Fatalf("the two known units get cards, the unknown one and the other site don't: %d in %s", len(cards), out)
	}
	var nova, grizzly catalog.UnitCard
	if err := json.Unmarshal([]byte(html.UnescapeString(cards[0][1])), &nova); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(html.UnescapeString(cards[1][1])), &grizzly); err != nil {
		t.Fatal(err)
	}
	if nova.ID != "BMBlackHawk_Prime" || nova.Name != "Nova" || nova.Model != "Prime" || nova.BV <= 0 || nova.Tons != 50 || !strings.HasPrefix(nova.Icon, "/icons/") {
		t.Errorf("the card is the catalog's unit, by its Clan name: %+v", nova)
	}
	if nova.Gunnery != 3 || nova.Piloting != 4 || nova.SkillBV != catalog.AdjustedBV(nova.BV, 3, 4) || nova.SkillBV <= nova.BV {
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
	if got := catalog.UnknownUnitLinks(out); len(got) != 1 || got[0] != "BMNoSuchMech_X" {
		t.Errorf("the unknown unit is reported: %v", got)
	}
}

func TestScenarioPagesHaveUnitCards(t *testing.T) {
	c, ok := content.Library{Root: "../../campaigns"}.Campaign("refusalwar")
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

// The weapon type bar counts weapons by type (the Atlas AS7-D: an AC/20, four medium lasers, an LRM 20 and an SRM 6), and the overview's hangar views
// carry the same card as a scenario's unit links.
func TestWeaponCountsAndTheOverviewCard(t *testing.T) {
	u, _ := catalog.ByName("BMAtlas_AS7D")
	if c := catalog.CardFor(u); c.Weapons == nil || *c.Weapons != (catalog.WeaponCounts{Ballistic: 1, Energy: 4, Missile: 2}) {
		t.Errorf("Atlas AS7-D weapons: %+v", c.Weapons)
	}
	views := hangarViews(mekbay.HangarForce{Units: []mekbay.HangarUnit{{ID: "u1", Unit: "BMAtlas_AS7D"}, {ID: "u2", Unit: "BMNoSuchMech_X"}}})
	if c := views[0].Card; c == nil || c.ID != "BMAtlas_AS7D" || c.Firepower != 72 || c.Weapons == nil || c.Gunnery != 0 {
		t.Errorf("a hangar unit carries its catalog card, with no skills: %+v", c)
	}
	if views[1].Card != nil {
		t.Error("an unknown unit has no card")
	}
}

// An omnimech's reconfigure options are marked available only when the campaign's faction fields the variant in its era (the same list as the
// purchase search), and all are available for a campaign that names no faction and era.
func TestSwapOptionsFollowAvailability(t *testing.T) {
	c, ok := content.Library{Root: "../../campaigns"}.Campaign("refusalwar")
	if !ok {
		t.Fatal("no Refusal War campaign")
	}
	avail := catalog.Available(c.AvailabilityFaction, c.AvailabilityEra)
	if avail == nil {
		t.Fatal("Refusal War names a faction and era MekBay knows")
	}
	views := hangarViews(mekbay.HangarForce{Units: []mekbay.HangarUnit{{ID: "u", Unit: "BMUller_A"}}})
	markSwapAvailability(views, c)
	var yes, no int
	for _, o := range views[0].SwapOptions {
		if o.Available != avail[o.Unit] {
			t.Errorf("%s: available %v, the faction list says %v", o.Unit, o.Available, avail[o.Unit])
		}
		if o.Available {
			yes++
		} else {
			no++
		}
	}
	if yes == 0 || no == 0 {
		t.Errorf("the Kit Fox has variants both in and out of Clan Wolf's Clan Invasion list: %d available, %d not", yes, no)
	}
	markSwapAvailability(views, &content.Campaign{})
	for _, o := range views[0].SwapOptions {
		if !o.Available {
			t.Errorf("with no faction and era every variant is available: %s", o.Unit)
		}
	}
}
