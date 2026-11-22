package site

import (
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/mekbay"
)

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
