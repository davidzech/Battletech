// Package content reads the campaign folders: each campaign's Intro.md, Keywords.md and Names.md, and its scenarios' Setup.md and
// Outcome.md (front matter checked strictly), their force files, and the outcome engine that turns a mission's report into rewards.
package content

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"gopkg.in/yaml.v3"

	"github.com/davidzech/Battletech/internal/catalog"
	"github.com/davidzech/Battletech/internal/format"
	"github.com/davidzech/Battletech/internal/pilot"
)

// Scenario is one Scenario_NNN folder.
type Scenario struct {
	Dir       string // absolute path
	Slug      string // URL segment, e.g. "scenario_000"
	ID        string // from front matter
	Number    string // "000"
	Title     string
	Map       string
	Planet    string
	Images    []string
	Readout   []ReadoutRow // planet readout overlaid on the first image
	Waypoints []Waypoint   // from the `## Objectives` section of Setup.md
	Forces    []ForceFile  // downloadable .mul files from front matter
	// ProvidedUnits, when set (front matter provided_units: MekBay unit IDs), replace the player's hangar for this scenario: the players pick from this fixed pool
	// instead, for story reasons. The units exist only for this scenario and never touch the hangar (see provided.go).
	ProvidedUnits []string
	// OutcomeInputs and OutcomeRules are the scenario's outcome engine (Outcome.md front matter `inputs` and `outcomes`, see outcomes.go); OutcomeError says what is wrong
	// with them, in which case the scenario has no engine.
	OutcomeInputs     []OutcomeInput
	OutcomeObjectives []OutcomeObjective
	// OutcomePilotSP is the flat SP each active pilot gets from the pool (half for a benched one, none for one killed; Outcome.md pilot_sp).
	OutcomePilotSP any
	pilotShare     *Expr
	OutcomeRules   []OutcomeRule
	OutcomeError   string
	// MVPSP is the extra SP the mission's MVP earns (the campaign's mvp_sp, or the scenario's own Outcome.md mvp_sp); 0 means the mission has no MVP.
	MVPSP int
	// Branches are the scenarios (slugs) this one may lead to once it is completed: the Outcome.md front matter `branches` list, or when that is omitted its
	// `default_next`, or else the next scenario number. Only a scenario that is the campaign's first, completed, or a branch of a completed one is available.
	Branches       []string
	branchesListed bool
	// UnitLimit is the most mechs the players may take onto the field in all (front matter unit_limit); 0 means no limit. PilotsRequired (pilots_required) makes
	// every mech taken need a named pilot assigned.
	UnitLimit      int
	PilotsRequired bool
	BVLimit        int           // most battle value the player may deploy (front matter bv_limit); 0 means no limit
	Next           string        // id of the default next scenario
	BriefingHTML   template.HTML // the fluff: a leading blockquote in Setup.md
	SetupHTML      template.HTML
	OutcomeHTML    template.HTML
}

// ReadoutRow is one line of the technical readout drawn over a scenario's image.
type ReadoutRow struct {
	Label string
	Value string
}

// Waypoint is one entry of a scenario's `## Objectives` section: a hidden event or objective that is only
// read when the game calls for it, so the page shows it redacted until the player reveals it.
type Waypoint struct {
	Name string        // e.g. "Objective 1"
	ID   string        // anchor-safe, e.g. "objective-1"
	HTML template.HTML // the rules text, with each text run wrapped for redaction
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
	// Keywords are the keyword IDs Keywords.md registers (the campaign's ledger can hold only these).
	Keywords   []string
	MinPlayers int // fewest players the campaign is built for (Intro.md front matter min_players); 0 if not given
	MaxPlayers int // most players it supports (max_players); 0 if not given
	// StartingNamedPilots is how many named pilots the campaign requires before scenarios can be played
	// (Intro.md front matter starting_named_pilots); 0 means none are required.
	StartingNamedPilots int
	// StartingPilotSP is the SP each of those pilots allocates, all of it, when the campaign is started (Intro.md
	// front matter starting_pilot_sp).
	StartingPilotSP int
	// StartingBV is the BV of the force a campaign starts with (Intro.md front matter starting_bv); 0 if not given.
	StartingBV int
	// StartingMinMechs is how many mechs the starting hangar must hold at least (Intro.md front matter
	// starting_min_mechs); 0 if not given.
	StartingMinMechs int
	// StartingWarchest is the SP the force's warchest starts the campaign with (Intro.md front matter starting_warchest); 0 if not given.
	StartingWarchest int
	// MaxNamedPilots is the most living named pilots the force may have at once (Intro.md max_named_pilots; Refusal War: 4); 0 means the default of four. RecruitCost is
	// the SP it costs from the warchest to recruit a new named pilot later on (Intro.md recruit_cost; Refusal War: 300); 0 is free. The starting pilots are free.
	MaxNamedPilots int
	RecruitCost    int
	// AvailabilityFaction and AvailabilityEra, e.g. "Clan Wolf" and "Clan Invasion", pick the units the purchase search offers by default (see
	// availability.go); both empty means no such filter.
	AvailabilityFaction, AvailabilityEra string
	// Faction is the side the player's force fights for (Intro.md front matter faction), which decides the ranks of its named
	// pilots; see factions.go. Empty or unknown turns ranks off.
	Faction string
	// Ranks is the rank ladder of the campaign's named pilots, lowest first: the Intro.md `ranks` list if it has one, else its
	// faction's.
	Ranks []string
	// MissionTerm is what the campaign calls its scenarios, e.g. "Sortie" (Intro.md mission_term; "Scenario" by default): see MissionName.
	MissionTerm string
	// MVPSP is the extra SP the most valuable pilot of a mission earns (Intro.md mvp_sp; 0 means the campaign has no MVP). A scenario's Outcome.md can override it.
	MVPSP int
	// Names are the campaign's own example force and pilot names (Names.md), which replace the site's baseline suggestions; see names.go.
	Names NameIdeas
	// Problems lists what is wrong with the campaign's files (front matter that doesn't parse or has unknown keys, outcome rules that don't compile, branches to
	// scenarios that don't exist), so an author sees them on the campaign page rather than a setting silently ignored. TestCampaignFilesAreValid keeps it empty.
	Problems  []string
	Scenarios []Scenario
}

// PlayersLabel describes how many players the campaign is for, e.g. "1-2 players", "1 player", or "" if the
// campaign doesn't say.
func (c *Campaign) PlayersLabel() string {
	lo, hi := c.MinPlayers, c.MaxPlayers
	if hi <= 0 {
		return ""
	}
	if lo <= 0 || lo > hi {
		lo = hi
	}
	switch {
	case lo == hi && hi == 1:
		return "1 player"
	case lo == hi:
		return strconv.Itoa(hi) + " players"
	}
	return strconv.Itoa(lo) + "-" + strconv.Itoa(hi) + " players"
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

// PilotLimit is the most living named pilots the force may have at once.
func (c *Campaign) PilotLimit() int {
	if c.MaxNamedPilots > 0 {
		return c.MaxNamedPilots
	}
	return pilot.MaxNamedPilots
}

// keywordBullet matches a keyword's bullet in Keywords.md: a lowercase snake_case ID in backticks at the start.
var keywordBullet = regexp.MustCompile("(?m)^\\s*[-*]\\s+`([a-z][a-z0-9_]*)`")

// keywordIDs lists the keyword IDs registered in a Keywords.md body.
func keywordIDs(body string) []string {
	var out []string
	for _, m := range keywordBullet.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// checkGrants marks a scenario whose outcome grants a keyword that Keywords.md doesn't register: it then has no outcome engine, and the mistake is reported.
func (c *Campaign) checkGrants() {
	known := map[string]bool{}
	for _, k := range c.Keywords {
		known[k] = true
	}
	for i := range c.Scenarios {
		sc := &c.Scenarios[i]
		if sc.OutcomeError != "" {
			continue
		}
		for _, g := range sc.GrantedKeywords() {
			if !known[g] {
				sc.OutcomeError = fmt.Sprintf("grants the keyword %q, which is not in Keywords.md", g)
				break
			}
		}
	}
}

// lowerAll lowercases and trims a list of scenario IDs.
func lowerAll(ids []string) []string {
	var out []string
	for _, id := range ids {
		if id = strings.ToLower(strings.TrimSpace(id)); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// resolveBranches fills in the branches of every scenario that doesn't list its own: its default_next if it has one, else the scenario with the next number (if
// there is one). A scenario that lists none ends that path of the campaign.
func (c *Campaign) resolveBranches() {
	for i := range c.Scenarios {
		sc := &c.Scenarios[i]
		if sc.branchesListed {
			continue
		}
		if next := strings.ToLower(strings.TrimSpace(sc.Next)); next != "" {
			sc.Branches = []string{next}
			continue
		}
		if n, err := strconv.Atoi(sc.Number); err == nil {
			slug := fmt.Sprintf("scenario_%0*d", len(sc.Number), n+1)
			if _, ok := c.Scenario(slug); ok {
				sc.Branches = []string{slug}
			}
		}
	}
}

// Available says which scenarios a run can see (and play): the campaign's first, every completed one, and every branch of a completed one. Keys are slugs;
// completed is the set of completed slugs.
func (c *Campaign) Available(completed map[string]bool) map[string]bool {
	out := map[string]bool{}
	if len(c.Scenarios) > 0 {
		out[c.Scenarios[0].Slug] = true
	}
	for slug := range completed {
		sc, ok := c.Scenario(slug)
		if !ok {
			continue
		}
		out[slug] = true
		for _, b := range sc.Branches {
			if _, ok := c.Scenario(b); ok {
				out[b] = true
			}
		}
	}
	return out
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

// introMeta is a campaign's Intro.md front matter: the campaign-wide settings (see Campaign).
type introMeta struct {
	MinPlayers          int      `yaml:"min_players"`
	MaxPlayers          int      `yaml:"max_players"`
	StartingNamedPilots int      `yaml:"starting_named_pilots"`
	StartingPilotSP     int      `yaml:"starting_pilot_sp"`
	StartingBV          int      `yaml:"starting_bv"`
	StartingMinMechs    int      `yaml:"starting_min_mechs"`
	StartingWarchest    int      `yaml:"starting_warchest"`
	MaxNamedPilots      int      `yaml:"max_named_pilots"`
	RecruitCost         int      `yaml:"recruit_cost"`
	AvailabilityFaction string   `yaml:"availability_faction"`
	AvailabilityEra     string   `yaml:"availability_era"`
	Faction             string   `yaml:"faction"`
	Ranks               []string `yaml:"ranks"`
	MissionTerm         string   `yaml:"mission_term"`
	MVPSP               int      `yaml:"mvp_sp"`
}

// setupMeta is a scenario's Setup.md front matter.
type setupMeta struct {
	ID        string              `yaml:"id"`
	Title     string              `yaml:"title"`
	Map       string              `yaml:"map"`
	Planet    string              `yaml:"planet"`
	Images    []string            `yaml:"images"`
	Readout   []map[string]string `yaml:"readout"`
	Forces    map[string]string   `yaml:"forces"`
	BVLimit   int                 `yaml:"bv_limit"`
	Provided  []string            `yaml:"provided_units"`
	UnitLimit int                 `yaml:"unit_limit"`
	PilotsReq bool                `yaml:"pilots_required"`
}

// outcomeMeta is a scenario's Outcome.md front matter: where it leads, and its outcome engine (see outcomes.go).
type outcomeMeta struct {
	ID          string             `yaml:"id"`
	DefaultNext string             `yaml:"default_next"`
	Branches    []string           `yaml:"branches"`
	Inputs      []OutcomeInput     `yaml:"inputs"`
	Objectives  []OutcomeObjective `yaml:"objectives"`
	PilotSP     any                `yaml:"pilot_sp"`
	Outcomes    []OutcomeRule      `yaml:"outcomes"`
	MVPSP       *int               `yaml:"mvp_sp"` // overrides the campaign's
}

// readMarkdown reads a Markdown file with optional YAML front matter into dst, and returns the body. The front matter is decoded strictly, so a misspelt or
// misplaced key is an error rather than a setting silently ignored; a missing file is not an error (it yields an empty body).
func readMarkdown(path string, dst any) (string, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	text := string(bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF}))
	m := frontMatter.FindStringSubmatch(text)
	if m == nil {
		return text, nil
	}
	dec := yaml.NewDecoder(strings.NewReader(m[1]))
	dec.KnownFields(true)
	if err := dec.Decode(dst); err != nil && err != io.EOF {
		return m[2], fmt.Errorf("front matter: %w", err)
	}
	return m[2], nil
}

func RenderMarkdown(src string, stripTitle bool) template.HTML {
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
	return template.HTML(catalog.AnnotateUnitLinks(out)) // a link to a unit's MekBay page carries the unit's details for its hover card (unitlinks.go)
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
	entries, err := os.ReadDir(l.Root)
	if err != nil {
		return nil, false
	}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && strings.ToLower(e.Name()) == slug {
			return l.load(e.Name()) // only this campaign is read, not every one
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
	problem := func(file string, err error) {
		if err != nil {
			c.Problems = append(c.Problems, file+": "+err.Error())
		}
	}
	var meta introMeta
	intro, err := readMarkdown(filepath.Join(dir, "Intro.md"), &meta)
	problem("Intro.md", err)
	c.applyIntro(meta)
	for _, e := range entries {
		m := scenarioDir.FindStringSubmatch(e.Name())
		if !e.IsDir() || m == nil {
			continue
		}
		sdir := filepath.Join(dir, e.Name())
		var setup setupMeta
		setupBody, err := readMarkdown(filepath.Join(sdir, "Setup.md"), &setup)
		problem(e.Name()+"/Setup.md", err)
		briefing, setupBody := splitBriefing(leadingTitle.ReplaceAllString(setupBody, ""))
		setupBody, waypoints := splitWaypoints(setupBody)
		var outcome outcomeMeta
		outcomeBody, err := readMarkdown(filepath.Join(sdir, "Outcome.md"), &outcome)
		problem(e.Name()+"/Outcome.md", err)
		s := Scenario{
			Dir:               sdir,
			Slug:              strings.ToLower(e.Name()),
			ID:                setup.ID,
			Number:            m[1],
			Title:             setup.Title,
			Map:               setup.Map,
			Planet:            setup.Planet,
			Images:            setup.Images,
			Readout:           readoutRows(setup.Readout),
			Waypoints:         waypoints,
			Forces:            forceFiles(setup.Forces),
			BVLimit:           setup.BVLimit,
			ProvidedUnits:     setup.Provided,
			UnitLimit:         setup.UnitLimit,
			PilotsRequired:    setup.PilotsReq,
			Next:              outcome.DefaultNext,
			OutcomeInputs:     outcome.Inputs,
			OutcomeObjectives: outcome.Objectives,
			OutcomePilotSP:    outcome.PilotSP,
			OutcomeRules:      outcome.Outcomes,
			MVPSP:             c.MVPSP,
			Branches:          lowerAll(outcome.Branches),
			branchesListed:    outcome.Branches != nil,
			BriefingHTML:      renderBriefing(briefing),
			SetupHTML:         RenderMarkdown(setupBody, false),
			OutcomeHTML:       RenderMarkdown(outcomeBody, true),
		}
		if outcome.MVPSP != nil {
			s.MVPSP = *outcome.MVPSP
		}
		if s.ID == "" {
			s.ID = s.Slug
		}
		s.CompileOutcomes()
		if s.Title == "" {
			s.Title = humanize(e.Name())
		}
		c.Scenarios = append(c.Scenarios, s)
	}
	if len(c.Scenarios) == 0 {
		return nil, false
	}
	sort.Slice(c.Scenarios, func(i, j int) bool { return c.Scenarios[i].Slug < c.Scenarios[j].Slug })
	c.resolveBranches()
	var kwMeta struct{}
	kw, err := readMarkdown(filepath.Join(dir, "Keywords.md"), &kwMeta)
	problem("Keywords.md", err)
	c.IntroHTML = RenderMarkdown(intro, false)
	c.KeywordsHTML = RenderMarkdown(kw, true)
	c.Keywords = keywordIDs(kw)
	c.loadNames(dir)
	c.checkGrants()
	c.checkScenarios()
	return c, true
}

// applyIntro takes the campaign-wide settings from Intro.md.
func (c *Campaign) applyIntro(meta introMeta) {
	c.MinPlayers, c.MaxPlayers = meta.MinPlayers, meta.MaxPlayers
	c.StartingNamedPilots, c.StartingPilotSP, c.StartingBV = meta.StartingNamedPilots, meta.StartingPilotSP, meta.StartingBV
	c.StartingMinMechs = meta.StartingMinMechs
	c.StartingWarchest = meta.StartingWarchest
	c.MaxNamedPilots, c.RecruitCost = meta.MaxNamedPilots, meta.RecruitCost
	c.AvailabilityFaction, c.AvailabilityEra = meta.AvailabilityFaction, meta.AvailabilityEra
	c.Faction = meta.Faction
	if c.Ranks = meta.Ranks; len(c.Ranks) == 0 {
		c.Ranks = pilot.FactionRanks(meta.Faction)
	}
	c.MissionTerm = strings.TrimSpace(meta.MissionTerm)
	if c.MissionTerm == "" {
		c.MissionTerm = "Scenario"
	}
	c.MVPSP = meta.MVPSP
}

// checkScenarios reports what is wrong with the scenarios as a whole: an outcome engine that doesn't compile, a link to a MekBay unit the catalog
// doesn't know, and a branch to a scenario that doesn't exist.
func (c *Campaign) checkScenarios() {
	for _, sc := range c.Scenarios {
		if sc.OutcomeError != "" {
			c.Problems = append(c.Problems, sc.Slug+"/Outcome.md: "+sc.OutcomeError)
		}
		for _, id := range catalog.UnknownUnitLinks(string(sc.BriefingHTML) + string(sc.SetupHTML)) {
			c.Problems = append(c.Problems, fmt.Sprintf("%s/Setup.md: links to the MekBay unit %q, which is not in the unit catalog", sc.Slug, id))
		}
		for _, b := range sc.Branches {
			if _, ok := c.Scenario(b); !ok {
				c.Problems = append(c.Problems, fmt.Sprintf("%s/Outcome.md: branches to %q, which is not a scenario of this campaign", sc.Slug, b))
			}
		}
	}
}

// FirstRank is the rank every named pilot starts at: the lowest of the campaign's ranks ("" when the campaign has none).
func (c *Campaign) FirstRank() string {
	if len(c.Ranks) == 0 {
		return ""
	}
	return c.Ranks[0]
}

// MissionNumber is how the campaign names a scenario by number, e.g. "Sortie 001" (Intro.md mission_term, "Scenario" by default).
func (c *Campaign) MissionNumber(sc Scenario) string { return c.MissionTerm + " " + sc.Number }

// MissionName is a scenario's full name, e.g. "Sortie 001: Pyre Light".
func (c *Campaign) MissionName(sc Scenario) string { return c.MissionNumber(sc) + ": " + sc.Title }

var (
	nonSlug     = regexp.MustCompile(`[^a-z0-9]+`)
	redactables = []*regexp.Regexp{
		regexp.MustCompile(`(?s)(<p>)(.*?)(</p>)`),
		regexp.MustCompile(`(?s)(<li>)(.*?)(</li>)`),
		regexp.MustCompile(`(?s)(<t[dh](?:\s[^>]*)?>)(.*?)(</t[dh]>)`),
	}
)

// splitWaypoints removes the `## Objectives` section from a Setup.md body and returns the rest of the body plus
// one Waypoint per `### ` heading inside that section. Text between the section heading and the first
// waypoint is dropped.
func splitWaypoints(body string) (rest string, wps []Waypoint) {
	var keep, cur []string
	var name string
	inSection := false
	flush := func() {
		if name == "" {
			return
		}
		wps = append(wps, Waypoint{
			Name: name,
			ID:   strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(name), "-"), "-"),
			HTML: redactable(RenderMarkdown(strings.Join(cur, "\n"), false)),
		})
		name, cur = "", nil
	}
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		switch {
		case strings.TrimSpace(line) == "## Objectives":
			inSection = true
		case inSection && strings.HasPrefix(line, "## "):
			flush()
			inSection = false
			keep = append(keep, line)
		case inSection && strings.HasPrefix(line, "### "):
			flush()
			name = strings.TrimSpace(strings.TrimPrefix(line, "### "))
		case inSection && name != "":
			cur = append(cur, line)
		case inSection:
			// intro text before the first waypoint is dropped
		default:
			keep = append(keep, line)
		}
	}
	flush()
	return strings.Join(keep, "\n"), wps
}

// redactable wraps the text of each paragraph, list item and table cell in a span the stylesheet can black
// out line by line, like a redacted document.
func redactable(html template.HTML) template.HTML {
	out := string(html)
	for _, re := range redactables {
		out = re.ReplaceAllString(out, `${1}<span class="redact">${2}</span>${3}`)
	}
	return template.HTML(out)
}

// readoutRows turns the front matter `readout` list (each item a single `Label: value` pair) into rows,
// keeping the file's order.
func readoutRows(items []map[string]string) []ReadoutRow {
	var out []ReadoutRow
	for _, item := range items {
		for label, value := range item {
			out = append(out, ReadoutRow{Label: label, Value: value})
		}
	}
	return out
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

// splitBriefing separates a leading blockquote (the scenario's fluff) from the rest of the body and
// returns the quote's text with the "> " markers removed.
func splitBriefing(body string) (briefing, rest string) {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	start := i
	for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), ">") {
		i++
	}
	if i == start {
		return "", body
	}
	quote := make([]string, 0, i-start)
	for _, l := range lines[start:i] {
		quote = append(quote, strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(l), ">"), " "))
	}
	return strings.Join(quote, "\n"), strings.Join(lines[i:], "\n")
}

// renderBriefing renders the fluff, marking any paragraph that opens with a quotation mark as a
// pull quote and any that opens with an em dash as its attribution, so they can be styled.
func renderBriefing(src string) template.HTML {
	html := string(RenderMarkdown(src, false))
	for _, open := range []string{"<p>&quot;", "<p>&ldquo;", "<p>\""} {
		html = strings.ReplaceAll(html, open, `<p class="pull">`+open[len("<p>"):])
	}
	html = strings.ReplaceAll(html, "<p>—", `<p class="cite">`+"—")
	return template.HTML(html)
}

// StartingWarchestText is the starting warchest with a thousands separator, e.g. "4,500".
func (c *Campaign) StartingWarchestText() string { return format.Commas(c.StartingWarchest) }

// StartingBVText is the starting BV with a thousands separator, e.g. "6,000".
func (c *Campaign) StartingBVText() string { return format.Commas(c.StartingBV) }
