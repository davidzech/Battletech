package site

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// Buttons never end in an ellipsis ("Deploy force", not "Deploy force..."), and Complete mission is the big outline button.
func TestButtonsHaveNoTrailingEllipsis(t *testing.T) {
	ellipsis := regexp.MustCompile(`(?s)<(button|a)\b[^>]*class="[^"]*\bbutton\b[^"]*"[^>]*>[^<]*(&hellip;|…|\.\.\.)\s*</(button|a)>`)
	srv, _, cookie := startedCompany(t)
	fresh := post(t, srv, "/api/seed", url.Values{"seed": {"freshseed002"}}, nil, "").Cookies()[0]
	for name, c := range map[string]struct {
		path   string
		cookie bool
	}{
		"the campaign page":     {"/refusalwar/", true},
		"the manage page":       {"/refusalwar/manage/", true},
		"a scenario (outcome)":  {"/refusalwar/scenario_000/", true},
		"a scenario (plain)":    {"/refusalwar/scenario_001/", true},
		"an unstarted campaign": {"/refusalwar/", false},
		"an unstarted scenario": {"/refusalwar/scenario_000/", false},
		"the keywords page":     {"/refusalwar/keywords/", true},
		"the home page":         {"/", true},
	} {
		ck := fresh
		if c.cookie {
			ck = cookie
		}
		if m := ellipsis.FindString(body(t, srv, c.path, ck)); m != "" {
			t.Errorf("%s has a button ending in an ellipsis: %s", name, m)
		}
	}
	page := body(t, srv, "/refusalwar/scenario_000/", cookie)
	if !strings.Contains(page, `class="button button-outline button-large" id="outcome-open">Complete mission</button>`) {
		t.Errorf("Complete mission is the big outline button: %.200s", page)
	}
	css := body(t, srv, "/static/style.css", nil)
	for _, want := range []string{".button.button-outline, button.button.button-outline { background: transparent; color: var(--accent); border: 1px solid var(--accent); clip-path: none; }", "border-width: 3px", ".mission-actions { display: flex; flex-wrap: wrap; align-items: center; justify-content: center;"} {
		if !strings.Contains(css, want) {
			t.Errorf("the stylesheet is missing %q", want)
		}
	}
}
