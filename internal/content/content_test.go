package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/pilot"
)

func TestStartingBVText(t *testing.T) {
	for bv, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 6000: "6,000", 12500: "12,500", 1234567: "1,234,567"} {
		if got := (&Campaign{StartingBV: bv}).StartingBVText(); got != want {
			t.Errorf("StartingBVText(%d) = %q, want %q", bv, got, want)
		}
	}
}

func TestPlayersLabel(t *testing.T) {
	for _, c := range []struct {
		lo, hi int
		want   string
	}{
		{1, 2, "1-2 players"},
		{2, 4, "2-4 players"},
		{1, 1, "1 player"},
		{3, 3, "3 players"},
		{0, 2, "2 players"}, // no minimum given: just the maximum
		{4, 2, "2 players"}, // nonsense range: trust the maximum
		{1, 0, ""},          // no maximum given: say nothing
		{0, 0, ""},
	} {
		if got := (&Campaign{MinPlayers: c.lo, MaxPlayers: c.hi}).PlayersLabel(); got != c.want {
			t.Errorf("PlayersLabel(%d, %d) = %q, want %q", c.lo, c.hi, got, c.want)
		}
	}
}

func TestSplitBriefing(t *testing.T) {
	body := "## Briefing\n\nFirst line.\n\n> Second.\n\n## Overview\n\nText.\n"
	rest, briefing := splitBriefing(body)
	if briefing != "First line.\n\n> Second." {
		t.Errorf("briefing = %q", briefing)
	}
	if strings.Contains(rest, "Briefing") || !strings.HasPrefix(strings.TrimSpace(rest), "## Overview") {
		t.Errorf("rest = %q", rest)
	}

	// No briefing section: a blockquote is just a blockquote and everything stays in the body.
	r, b := splitBriefing("> not fluff\n\n## Overview\n")
	if b != "" || !strings.Contains(r, "> not fluff") || !strings.Contains(r, "## Overview") {
		t.Errorf("unexpected split: %q / %q", r, b)
	}

	// The section runs to the end of the body, and Windows line endings are fine.
	if r, b := splitBriefing("Before.\r\n## Briefing\r\nA\r\nB\r\n"); b != "A\nB" || strings.TrimSpace(r) != "Before." {
		t.Errorf("CRLF split = %q / %q", r, b)
	}
}

func TestRenderBriefingMarksQuotesNotDialogue(t *testing.T) {
	html := string(renderBriefing("\"Move out,\" she said.\n\n— Not an attribution\n\n> If you are victorious, so be it.\n>\n> — Someone, Title"))
	if strings.Count(html, "<blockquote>") != 1 || strings.Count(html, `class="cite"`) != 1 || !strings.Contains(html, `<p class="cite">— Someone, Title</p>`) {
		t.Errorf("html = %s", html)
	}
	if strings.Contains(html, `class="pull"`) || !strings.Contains(html, "<p>— Not an attribution</p>") {
		t.Errorf("text outside the quote was marked: %s", html)
	}

	// "--" is an easier-to-type em dash for the attribution (a single "- " would be a list item).
	html = string(renderBriefing("> Quote.\n>\n> -- Someone, Title"))
	if !strings.Contains(html, `<p class="cite">— Someone, Title</p>`) {
		t.Errorf("-- attribution: %s", html)
	}
}

func TestSplitWaypoints(t *testing.T) {
	body := "## Map\n\nText.\n\n## Objectives\n\nIgnored intro.\n\n### Objective 1\n\nFirst *one*.\n\n+100 SP.\n\n### Objective 2\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n## Later\n\nMore.\n"
	rest, wps := splitWaypoints(body)
	if strings.Contains(rest, "Objective") || !strings.Contains(rest, "## Map") || !strings.Contains(rest, "## Later") {
		t.Errorf("rest = %q", rest)
	}
	if len(wps) != 2 || wps[0].Name != "Objective 1" || wps[0].ID != "objective-1" || wps[1].Name != "Objective 2" {
		t.Fatalf("waypoints = %+v", wps)
	}
	a := string(wps[0].HTML)
	if strings.Count(a, `<span class="redact">`) != 2 || !strings.Contains(a, `<span class="redact">First <em>one</em>.</span>`) {
		t.Errorf("objective 1 html = %s", a)
	}
	if strings.Contains(a, "Ignored intro") || !strings.Contains(string(wps[1].HTML), `<td><span class="redact">1</span></td>`) {
		t.Errorf("intro kept or table cell not wrapped: %s / %s", a, wps[1].HTML)
	}
	if rest, wps := splitWaypoints("## Map\n\nText.\n"); rest != "## Map\n\nText.\n" || len(wps) != 0 {
		t.Errorf("no section: rest = %q, waypoints = %v", rest, wps)
	}
}

func TestScenarioPlanetFromFrontMatter(t *testing.T) {
	c, ok := Library{Root: "../../campaigns"}.Campaign("refusalwar")
	if !ok {
		t.Fatal("refusalwar campaign not found")
	}
	sc, ok := c.Scenario("scenario_000")
	if !ok || sc.Planet != "Strana Mechty" {
		t.Fatalf("scenario_000 planet = %q", sc.Planet)
	}
}

func TestRefusalWarStartsWithTwoNamedPilots(t *testing.T) {
	c, ok := (Library{Root: "../../campaigns"}).Campaign("refusalwar")
	if !ok || c.StartingNamedPilots != 2 || c.StartingPilotSP != 150 || c.StartingBV != 10000 || c.StartingMinMechs != 5 {
		t.Fatalf("Refusal War starts with two named pilots (150 SP each) and a 10000 BV force: %+v", c)
	}
}

// Each campaign picks its faction (and so its ranks) in Intro.md; a campaign may also list its own ranks.
func TestCampaignFactionAndRanks(t *testing.T) {
	rw := refusalWar(t)
	// Refusal War lists its own ladder: the Clan ranks with Cadet below MechWarrior. Its pilots start as cadets and recruits join as MechWarriors.
	want := []string{"Cadet", "MechWarrior", "Star Commander", "Star Captain", "Star Colonel", "Galaxy Commander", "saKhan", "Khan"}
	if rw.Faction != "clan" || strings.Join(rw.Ranks, "|") != strings.Join(want, "|") || rw.FirstRank() != "Cadet" || rw.RecruitRank() != "MechWarrior" {
		t.Fatalf("Refusal War: faction %q, ranks %v, first %q, recruits %q", rw.Faction, rw.Ranks, rw.FirstRank(), rw.RecruitRank())
	}
	// The returned ladders are copies: changing one never changes the faction.
	rw.Ranks[0] = "X"
	if pilot.FactionRanks("clan")[0] != "MechWarrior" {
		t.Error("factionRanks should hand out copies")
	}
	if pilot.FactionRanks("nope") != nil || pilot.FactionRanks("") != nil {
		t.Error("an unknown or empty faction has no ranks")
	}

	root := t.TempDir()
	mk := func(slug, intro string) {
		dir := filepath.Join(root, slug)
		if err := os.MkdirAll(filepath.Join(dir, "Scenario_000"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "Intro.md"), []byte(intro), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("plain", "---\nmin_players: 1\n---\n")
	mk("unknown", "---\nfaction: martians\n---\n")
	mk("custom", "---\nfaction: clan\nranks:\n  - Sergeant\n  - Lieutenant\n  - Captain\n---\n")
	lib := Library{Root: root}
	if c, ok := lib.Campaign("plain"); !ok || c.Ranks != nil || c.Faction != "" {
		t.Errorf("a campaign with no faction has no ranks: %+v", c)
	}
	if c, ok := lib.Campaign("unknown"); !ok || c.Ranks != nil {
		t.Errorf("an unknown faction turns ranks off: %+v", c)
	}
	if c, ok := lib.Campaign("custom"); !ok || strings.Join(c.Ranks, "|") != "Sergeant|Lieutenant|Captain" {
		t.Errorf("a campaign's own ranks replace its faction's: %+v", c)
	}
}

func TestScenarioBVLimit(t *testing.T) {
	c, ok := (Library{Root: "../../campaigns"}).Campaign("refusalwar")
	if !ok {
		t.Fatal("no campaign")
	}
	if sc, _ := c.Scenario("scenario_001"); sc.BVLimit != 7500 {
		t.Errorf("scenario 001 BV limit = %d, want 7500", sc.BVLimit)
	}
	if sc, _ := c.Scenario("scenario_000"); sc.BVLimit != 0 {
		t.Errorf("scenario 000 should have no BV limit, got %d", sc.BVLimit)
	}
}
