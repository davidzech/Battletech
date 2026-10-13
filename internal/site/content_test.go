package site

import (
	"strings"
	"testing"
)

func TestSplitBriefing(t *testing.T) {
	body := "\n> First line.\n>\n> \"Second.\"\n\n## Overview\n\nText.\n"
	briefing, rest := splitBriefing(body)
	if briefing != "First line.\n\n\"Second.\"" {
		t.Errorf("briefing = %q", briefing)
	}
	if !strings.HasPrefix(strings.TrimSpace(rest), "## Overview") {
		t.Errorf("rest = %q", rest)
	}

	// No leading blockquote: everything stays in the body.
	b, r := splitBriefing("## Overview\n\n> not fluff\n")
	if b != "" || !strings.Contains(r, "## Overview") || !strings.Contains(r, "> not fluff") {
		t.Errorf("unexpected split: %q / %q", b, r)
	}

	// Windows line endings.
	if b, _ := splitBriefing("> A\r\n> B\r\n\r\nrest"); b != "A\nB" {
		t.Errorf("CRLF briefing = %q", b)
	}
}

func TestRenderBriefingMarksPullQuote(t *testing.T) {
	html := string(renderBriefing("Plain line.\n\n\"If you are victorious, so be it.\""))
	if strings.Count(html, `class="pull"`) != 1 || !strings.Contains(html, "<p>Plain line.</p>") {
		t.Errorf("html = %s", html)
	}
}

func TestRenderBriefingMarksAttribution(t *testing.T) {
	html := string(renderBriefing("\"Quote.\"\n\n— Someone, Title"))
	if strings.Count(html, `class="cite"`) != 1 || !strings.Contains(html, "— Someone, Title") {
		t.Errorf("html = %s", html)
	}
}

func TestSplitWaypoints(t *testing.T) {
	body := "## Map\n\nText.\n\n## Waypoints\n\nIgnored intro.\n\n### Waypoint A\n\nFirst *one*.\n\n+100 SP.\n\n### Waypoint B\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n## Later\n\nMore.\n"
	rest, wps := splitWaypoints(body)
	if strings.Contains(rest, "Waypoint") || !strings.Contains(rest, "## Map") || !strings.Contains(rest, "## Later") {
		t.Errorf("rest = %q", rest)
	}
	if len(wps) != 2 || wps[0].Name != "Waypoint A" || wps[0].ID != "waypoint-a" || wps[1].Name != "Waypoint B" {
		t.Fatalf("waypoints = %+v", wps)
	}
	a := string(wps[0].HTML)
	if strings.Count(a, `<span class="redact">`) != 2 || !strings.Contains(a, `<span class="redact">First <em>one</em>.</span>`) {
		t.Errorf("waypoint A html = %s", a)
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
