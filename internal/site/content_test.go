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
