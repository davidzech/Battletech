// Package site serves the campaign folders as a website.
package site

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"gopkg.in/yaml.v3"
)

// Scenario is one Scenario_NNN folder.
type Scenario struct {
	Dir         string // absolute path
	Slug        string // URL segment, e.g. "scenario_000"
	ID          string // from front matter
	Number      string // "000"
	Title       string
	Map         string
	Images      []string
	Forces      []ForceFile // downloadable .mul files from front matter
	Next        string      // id of the default next scenario
	SetupHTML   template.HTML
	OutcomeHTML template.HTML
}

// ForceFile is a .mul force file offered for download on a scenario page.
type ForceFile struct {
	Label string // e.g. "OpFor 1"
	File  string // file name inside the scenario folder
}

// Campaign is a folder containing Scenario_NNN folders.
type Campaign struct {
	Slug         string
	Label        string
	IntroHTML    template.HTML
	KeywordsHTML template.HTML
	Scenarios    []Scenario
}

// Scenario returns the scenario with the given slug.
func (c *Campaign) Scenario(slug string) (*Scenario, bool) {
	for i := range c.Scenarios {
		if c.Scenarios[i].Slug == slug {
			return &c.Scenarios[i], true
		}
	}
	return nil, false
}

// ScenarioByID returns the scenario with the given front matter id.
func (c *Campaign) ScenarioByID(id string) (*Scenario, bool) {
	for i := range c.Scenarios {
		if c.Scenarios[i].ID == id {
			return &c.Scenarios[i], true
		}
	}
	return nil, false
}

// Library reads campaigns from a content root (the campaigns folder).
type Library struct{ Root string }

var (
	scenarioDir  = regexp.MustCompile(`(?i)^scenario_(\d+)$`)
	frontMatter  = regexp.MustCompile(`(?s)\A---\r?\n(.*?)\r?\n---\r?\n?(.*)\z`)
	leadingTitle = regexp.MustCompile(`\A\s*#\s.*\r?\n`)
	opforKey     = regexp.MustCompile(`(?i)^opfor(\d+)$`)
	camelGap     = regexp.MustCompile(`([a-z])([A-Z])`)
	externalLink = regexp.MustCompile(`<a href="(https?://[^"]+)"`)
	md           = goldmark.New(goldmark.WithExtensions(extension.GFM))
)

func humanize(name string) string {
	return strings.ReplaceAll(camelGap.ReplaceAllString(name, "$1 $2"), "_", " ")
}

type frontData struct {
	ID          string            `yaml:"id"`
	Title       string            `yaml:"title"`
	Map         string            `yaml:"map"`
	Images      []string          `yaml:"images"`
	Forces      map[string]string `yaml:"forces"`
	DefaultNext string            `yaml:"default_next"`
}

// readMarkdown splits optional YAML front matter from a Markdown file. A missing file yields zero values.
func readMarkdown(path string) (frontData, string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return frontData{}, ""
	}
	text := string(bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF}))
	var fm frontData
	if m := frontMatter.FindStringSubmatch(text); m != nil {
		_ = yaml.Unmarshal([]byte(m[1]), &fm)
		text = m[2]
	}
	return fm, text
}

func renderMarkdown(src string, stripTitle bool) template.HTML {
	if stripTitle {
		src = leadingTitle.ReplaceAllString(src, "")
	}
	if strings.TrimSpace(src) == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return ""
	}
	// External links (MekBay etc.) open in a new tab.
	out := externalLink.ReplaceAllString(buf.String(), `<a href="$1" target="_blank" rel="noopener noreferrer"`)
	return template.HTML(out)
}

// Campaigns lists every campaign, sorted by folder name.
func (l Library) Campaigns() ([]*Campaign, error) {
	entries, err := os.ReadDir(l.Root)
	if err != nil {
		return nil, err
	}
	var out []*Campaign
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if c, ok := l.load(e.Name()); ok {
			out = append(out, c)
		}
	}
	return out, nil
}

// Campaign finds a campaign by its URL slug (lowercased folder name).
func (l Library) Campaign(slug string) (*Campaign, bool) {
	cs, err := l.Campaigns()
	if err != nil {
		return nil, false
	}
	for _, c := range cs {
		if c.Slug == slug {
			return c, true
		}
	}
	return nil, false
}

func (l Library) load(name string) (*Campaign, bool) {
	dir := filepath.Join(l.Root, name)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, false
	}
	c := &Campaign{Slug: strings.ToLower(name), Label: humanize(name)}
	for _, e := range entries {
		m := scenarioDir.FindStringSubmatch(e.Name())
		if !e.IsDir() || m == nil {
			continue
		}
		sdir := filepath.Join(dir, e.Name())
		setup, setupBody := readMarkdown(filepath.Join(sdir, "Setup.md"))
		outcome, outcomeBody := readMarkdown(filepath.Join(sdir, "Outcome.md"))
		s := Scenario{
			Dir:         sdir,
			Slug:        strings.ToLower(e.Name()),
			ID:          setup.ID,
			Number:      m[1],
			Title:       setup.Title,
			Map:         setup.Map,
			Images:      setup.Images,
			Forces:      forceFiles(setup.Forces),
			Next:        outcome.DefaultNext,
			SetupHTML:   renderMarkdown(setupBody, true),
			OutcomeHTML: renderMarkdown(outcomeBody, true),
		}
		if s.ID == "" {
			s.ID = s.Slug
		}
		if s.Title == "" {
			s.Title = humanize(e.Name())
		}
		c.Scenarios = append(c.Scenarios, s)
	}
	if len(c.Scenarios) == 0 {
		return nil, false
	}
	sort.Slice(c.Scenarios, func(i, j int) bool { return c.Scenarios[i].Slug < c.Scenarios[j].Slug })
	_, intro := readMarkdown(filepath.Join(dir, "Intro.md"))
	_, kw := readMarkdown(filepath.Join(dir, "Keywords.md"))
	c.IntroHTML = renderMarkdown(intro, false)
	c.KeywordsHTML = renderMarkdown(kw, true)
	return c, true
}

// forceFiles turns the front matter `forces` map (opfor1: OpFor1.mul) into an ordered list.
// Only plain file names are kept, so a scenario can't point downloads outside its folder.
func forceFiles(m map[string]string) []ForceFile {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []ForceFile
	for _, k := range keys {
		file := m[k]
		if file == "" || file != filepath.Base(file) {
			continue
		}
		label := k
		if g := opforKey.FindStringSubmatch(k); g != nil {
			label = "OpFor " + g[1]
		}
		out = append(out, ForceFile{Label: label, File: file})
	}
	return out
}
