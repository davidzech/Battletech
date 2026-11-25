package site

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"time"

	"github.com/davidzech/Battletech/internal/content"
	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/mekbayproxy"
	"github.com/davidzech/Battletech/internal/pilot"
	"github.com/davidzech/Battletech/internal/storage"
)

//go:embed templates/*.html static/*
var assets embed.FS

// staticETags maps each embedded static file to a hash of its contents. Embedded files have no
// modification time, so without an ETag browsers can't revalidate them and re-download every
// file (fonts included) on each refresh.
var staticETags = func() map[string]string {
	tags := map[string]string{}
	files, _ := assets.ReadDir("static")
	for _, f := range files {
		b, err := assets.ReadFile("static/" + f.Name())
		if err != nil {
			continue
		}
		sum := sha256.Sum256(b)
		tags[f.Name()] = `"` + hex.EncodeToString(sum[:8]) + `"`
	}
	return tags
}()

// Crumb is one breadcrumb link. A crumb with a Menu opens a dropdown of sibling pages
// (the campaign's scenarios) instead of linking straight to URL; Current marks the open page.
type Crumb struct {
	Label, URL string
	Current    bool
	Menu       []Crumb
}

// Page is the data every template receives.
type Page struct {
	Title string
	// BackURL and BackLabel, when set, replace the Manage page's "Back to the campaign" link (it returns to the scenario the player came from).
	BackURL, BackLabel string
	Crumbs             []Crumb
	Campaign           *content.Campaign
	OnManagePage       bool // the Manage force page itself, which has no Manage force button in its header
	Scenario           *content.Scenario
	Cards              []Card
	HasLocked          bool                   // some scenario cards are locked and hidden, so the campaign page offers 'Show all scenarios'
	GraphLanes         int                    // how many lanes the campaign page's path graph needs
	ScenarioState      string                 // on a scenario page: completed, available or locked
	NextScenarios      []content.Scenario     // on a completed scenario's page: the scenarios it leads to, which are now available
	Outcome            *content.OutcomeResult // on a completed scenario page: the recorded report of its outcome
	Images             []string               // URLs of the scenario's images
	Seed               string                 // the visitor's seed ("" if none chosen yet)
	Play               storage.Playthrough    // the visitor's playthrough (zero value if no seed)
	Run                storage.CampaignRun    // the playthrough's run of this page's campaign (zero value until it is started)
	History            []historyView          // a started campaign's history, newest first (the campaign page's History dialog)
	HistoryFilters     []historyFilter        // the kinds the history holds, as the dialog's filter buttons
	Pilots             []pilot.PilotView      // the run's named pilots
	Keywords           []KeywordView          // on the campaign overview: the keywords the campaign has earned, oldest first
	CanRecruit         bool                   // the command has room for another living named pilot, and the warchest can pay for them
	RecruitNote        string                 // the short gray note under Recruit New Pilot: why it is unavailable, or what it costs
	MaxPilots          int                    // most living named pilots a command may have
	// CachedHangar is the hangar last read from MekBay for this run (nil if none yet), and CachedAt when; the campaign and scenario
	// pages embed it so they can paint the hangar before the live read finishes (see hangarcache.go).
	CachedHangar   *mekbay.HangarForce
	CachedAt       time.Time
	Ranks          []string        // the campaign's pilot ranks, lowest first (empty when it has none)
	Memorial       bool            // at least one named pilot was killed in action
	Ladders        string          // the pilot improvement ladders as JSON, for the pilot builder's live preview
	Abilities      string          // the Edge ability catalog as JSON, for the ability picker
	Portraits      string          // the portrait list as JSON, for the portrait picker
	InProgress     bool            // the scenario has a battle in progress (an operation that is neither completed nor aborted)
	OperationURL   string          // the table's link to that battle, so the Operation in progress view can show it before anything is read from MekBay
	KeywordChoices []KeywordChoice // every registered keyword, ticked when held (the Manage page's Edit keywords dialog)
	BattleCache    string          // the last status read of that battle as JSON ({forces, updatedAt}), so the view paints it at once while a fresh read is made
	Commit         string          // git commit the server was built from, short form ("dev" if unknown)
	DevReload      bool            // the server runs with -dev-reload, so the layout loads static/devreload.js
}

// Card is one entry in a list of links (campaigns or scenarios).
type Card struct {
	Num, Name, Sub, URL, State string // State: "completed", "available" or "locked" for a scenario card
	// A scenario card's slice of the campaign page's path graph (graph.go), and whether a locked card is shown anyway: a mission the run passed over (offered
	// but not taken, or numbered below the last completed one), which the graph needs on screen.
	Graph  template.HTML
	Passed bool
	// A campaign card's emblem and era: Icon is the campaign's icon URL, Era its era's name and EraIcon that era's icon URL ("" for an era with no icon).
	Icon, Era, EraIcon string
	EraYears           string // the era's years, e.g. "3050–3061" ("" for an era the site doesn't know)
}

// eras are the BattleTech eras a campaign can be set in (its Intro.md availability_era), with their years (from MekBay's data/eras.json; to 0 for an era still
// open) and, for some, an icon embedded in static/. An era not listed is shown by name only.
var eras = map[string]struct {
	from, to int
	icon     string
}{
	"Age of War":                        {2005, 2570, ""},
	"Star League":                       {2571, 2780, ""},
	"Early Succession War":              {2781, 2900, ""},
	"Late Succession War - LosTech":     {2901, 3019, ""},
	"Late Succession War - Renaissance": {3020, 3049, ""},
	"Clan Invasion":                     {3050, 3061, "era-clan-invasion.webp"},
	"Civil War":                         {3062, 3067, ""},
	"Jihad":                             {3068, 3080, ""},
	"Early Republic":                    {3081, 3100, ""},
	"Late Republic":                     {3101, 3130, ""},
	"Dark Age":                          {3131, 3150, ""},
	"ilClan":                            {3151, 0, ""},
}

// campaignCard is a campaign as the home page lists it: its name, how many players it is for, its emblem and its era.
func campaignCard(c *content.Campaign) Card {
	card := Card{Name: c.Label, Sub: c.PlayersLabel(), URL: "/" + c.Slug + "/", Era: c.AvailabilityEra, Icon: c.IconURL()}
	if e, ok := eras[c.AvailabilityEra]; ok {
		if e.icon != "" {
			card.EraIcon = "/static/" + e.icon
		}
		if e.to == 0 {
			card.EraYears = fmt.Sprintf("%d–", e.from)
		} else {
			card.EraYears = fmt.Sprintf("%d–%d", e.from, e.to)
		}
	}
	return card
}

// Server serves the campaigns found under the content root.
type Server struct {
	lib    content.Library
	store  *storage.Store
	pages  map[string]*template.Template
	mekbay mekbay.MekBay

	// The MekBay proxy (mekbayproxy.go), served on the hosts mekbay-<link id>.<site host> (and mekbay.localhost).
	upstream *[3]string // web, WebSocket and Origin of the MekBay to proxy; tests replace it
	proxy    *mekbayproxy.Proxy
	onProxy  func(*mekbayproxy.Proxy) // tests take hold of the proxy here

	bootID string // set by WithDevReload: this process's ID for live reload (devreload.go); empty means off
}

// Option customizes the handler returned by NewHandler.
type Option func(*Server)

// WithMekBay replaces the MekBay client (tests use a fake one).
func WithMekBay(m mekbay.MekBay) Option { return func(s *Server) { s.mekbay = m } }

// NewHandler returns the site's HTTP handler. Campaign files are re-read on every request,
// so edits show up on refresh.
func NewHandler(contentRoot string, store *storage.Store, opts ...Option) http.Handler {
	s := &Server{lib: content.Library{Root: contentRoot}, store: store, pages: map[string]*template.Template{}, mekbay: &mekbay.Client{}}
	for _, o := range opts {
		o(s)
	}
	s.proxy = mekbayproxy.New(store)
	if s.upstream != nil {
		s.proxy.Web, s.proxy.WS, s.proxy.Origin = s.upstream[0], s.upstream[1], s.upstream[2]
	}
	if s.onProxy != nil {
		s.onProxy(s.proxy)
	}
	for _, name := range []string{"home", "campaign", "manage", "scenario", "notfound"} {
		s.pages[name] = template.Must(template.ParseFS(assets, "templates/layout.html", "templates/"+name+".html"))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /icons/{file}", s.icon)
	mux.HandleFunc("GET /portraits/{file}", s.portrait)
	mux.HandleFunc("GET /static/{file}", func(w http.ResponseWriter, r *http.Request) {
		name := path.Base(r.PathValue("file"))
		if tag, ok := staticETags[name]; ok {
			w.Header().Set("ETag", tag)
		}
		http.ServeFileFS(w, r, assets, path.Join("static", name))
	})
	// Browsers ask for /favicon.ico on their own (for pages without the layout's icon link, such as an image or a JSON reply): answer with the same PNG.
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", staticETags["favicon.png"])
		http.ServeFileFS(w, r, assets, "static/favicon.png")
	})
	mux.HandleFunc("POST /api/seed", s.setSeed)
	mux.HandleFunc("POST /api/account", s.setAccount)
	mux.HandleFunc("POST /api/campaign/start", s.startCampaign)
	mux.HandleFunc("POST /api/campaign/delete", s.deleteCampaign)
	mux.HandleFunc("GET /api/campaign/hangar-check", s.hangarCheck)
	mux.HandleFunc("GET /api/campaign/force", s.forceJSON)
	mux.HandleFunc("POST /api/pilots", s.addPilot)
	mux.HandleFunc("POST /api/pilots/allocate", s.allocateSP)
	mux.HandleFunc("POST /api/pilots/fallen", s.pilotFallen)
	mux.HandleFunc("POST /api/pilots/revive", s.revivePilot)
	mux.HandleFunc("POST /api/pilots/reset", s.resetPilotSP)
	mux.HandleFunc("POST /api/pilots/rename", s.renamePilot)
	mux.HandleFunc("POST /api/pilots/rank", s.setRank)
	mux.HandleFunc("POST /api/pilots/abilities", s.learnAbilities)
	mux.HandleFunc("POST /api/pilots/abilities/reset", s.resetEdgeAbilities)
	mux.HandleFunc("POST /api/pilots/total", s.setPilotTotal)
	mux.HandleFunc("POST /api/warchest", s.setWarchest)
	mux.HandleFunc("POST /api/campaign/players", s.setPlayers)
	mux.HandleFunc("POST /api/campaign/rollback", s.rollbackCampaign)
	mux.HandleFunc("POST /api/campaign/keywords", s.setKeywords)
	mux.HandleFunc("POST /api/campaign/force-name", s.setForceName)
	mux.HandleFunc("POST /api/campaign/scenario-complete", s.setScenarioCompleted)
	mux.HandleFunc("POST /api/campaign/scenario-outcome", s.previewOutcome)
	mux.HandleFunc("POST /api/campaign/scenario-outcome/commit", s.commitOutcome)
	mux.HandleFunc("POST /api/hangar/service", s.serviceUnit)
	mux.HandleFunc("POST /api/hangar/purchase", s.purchaseUnits)
	mux.HandleFunc("POST /api/hangar/sell", s.sellUnit)
	mux.HandleFunc("POST /api/hangar/swap", s.swapUnit)
	mux.HandleFunc("POST /api/deploy/plan", s.deployPlan)
	mux.HandleFunc("POST /api/battle/start", s.startBattle)
	mux.HandleFunc("GET /api/battle/status", s.battleStatus)
	mux.HandleFunc("POST /api/battle/abort", s.abortBattle)
	mux.HandleFunc("GET /api/units/search", s.searchUnitsHandler)
	mux.HandleFunc("POST /api/pilots/delete", s.deletePilot)
	mux.HandleFunc("POST /api/pilots/portrait", s.setPortrait)
	mux.HandleFunc("GET /api/mekbay/hangar", s.hangarJSON)
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /{campaign}/{$}", s.campaign)
	mux.HandleFunc("GET /{campaign}/manage/{$}", s.manage)
	mux.HandleFunc("GET /{campaign}/{scenario}/{$}", s.scenario)
	mux.HandleFunc("GET /{campaign}/images/{file}", s.campaignImage)
	mux.HandleFunc("GET /{campaign}/{scenario}/images/{file}", s.image)
	mux.HandleFunc("GET /{campaign}/{scenario}/forces/{file}", s.force)
	if s.bootID != "" {
		mux.HandleFunc("GET /dev/reload", s.devReload)
	}
	mux.HandleFunc("/", s.notFound)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mekbayproxy.IsHost(r.Host) {
			s.proxy.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, name string, p Page) {
	if seed := currentSeed(r); seed != "" {
		p.Seed = seed
		id := playthroughID(seed)
		if _, err := s.store.EnsurePlaythrough(r.Context(), id); err != nil {
			log.Printf("ensure playthrough: %v", err)
		} else if pt, _, err := s.store.Playthrough(r.Context(), id); err != nil {
			log.Printf("load playthrough: %v", err)
		} else {
			p.Play = pt
			if p.Campaign != nil {
				p.Ladders, p.Portraits, p.Abilities = pilot.LaddersJSON, pilot.PortraitsJSON(), pilot.AbilitiesJSON
				if run, _, err := s.store.Run(r.Context(), id, p.Campaign.Slug); err != nil {
					log.Printf("load campaign run: %v", err)
				} else {
					p.Run = run
				}
				if p.Run.Started() {
					pilots, err := s.store.Pilots(r.Context(), id, p.Campaign.Slug)
					if err != nil {
						log.Printf("load named pilots: %v", err)
					}
					var alive int
					p.Pilots, alive = pilot.Views(pilots, p.Campaign.Ranks)
					p.CanRecruit, p.RecruitNote = recruitStatus(p.Campaign, p.Run, alive)
					limit := p.Campaign.PilotLimit()
					p.Ranks = p.Campaign.Ranks
					if name == "campaign" || name == "scenario" || name == "manage" {
						if h, at, ok, err := s.store.CachedHangar(r.Context(), id, p.Campaign.Slug, p.Run.HangarInstance); err != nil {
							log.Printf("load cached hangar: %v", err)
						} else if ok {
							p.CachedHangar, p.CachedAt = &h, at
						}
					}
					for _, v := range p.Pilots {
						p.Memorial = p.Memorial || v.Fallen
					}
					if name == "campaign" || name == "manage" {
						p.Keywords = s.keywordViews(r, id, p.Campaign)
					}
					if name == "campaign" {
						p.History, p.HistoryFilters = s.historyViews(r, id, p.Campaign)
						p.KeywordChoices = s.keywordChoices(r, id, p.Campaign) // the Danger zone's Edit keywords
					}
					p.MaxPilots = limit
				}
			}
		}
	}
	p.Commit = shortCommit(buildCommit)
	p.DevReload = s.bootID != ""
	// The page is rendered in full before anything is sent, so a template error is a clean 500 rather than half a page under a 200.
	var buf bytes.Buffer
	if err := s.pages[name].ExecuteTemplate(&buf, "layout", p); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "could not render the page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusNotFound, "notfound", Page{Title: "Not found | Campaigns"})
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	cs, err := s.lib.Campaigns()
	if err != nil {
		http.Error(w, "cannot read campaigns", http.StatusInternalServerError)
		log.Print(err)
		return
	}
	p := Page{Title: "Campaigns"}
	for _, c := range cs {
		p.Cards = append(p.Cards, campaignCard(c))
	}
	s.render(w, r, http.StatusOK, "home", p)
}

func (s *Server) campaign(w http.ResponseWriter, r *http.Request) {
	c, ok := s.lib.Campaign(r.PathValue("campaign"))
	if !ok {
		s.notFound(w, r)
		return
	}
	p := Page{
		Title:    c.Label + " | Campaigns",
		Crumbs:   []Crumb{{Label: c.Label, URL: "/" + c.Slug + "/"}},
		Campaign: c,
	}
	completed, available := s.progress(r, c)
	graph, kinds, lanes := scenarioGraph(c.Scenarios, completed, available)
	p.GraphLanes = lanes
	lastDone := -1
	for i, sc := range c.Scenarios {
		if completed[sc.Slug] {
			lastDone = i
		}
	}
	for i, sc := range c.Scenarios {
		state := scenarioState(sc.Slug, completed, available)
		passed := state == "locked" && (kinds[i] == graphPassed || i < lastDone)
		p.HasLocked = p.HasLocked || (state == "locked" && !passed)
		p.Cards = append(p.Cards, Card{Num: sc.Number, Name: sc.Title, Sub: sc.Planet, URL: "/" + c.Slug + "/" + sc.Slug + "/", State: state, Graph: graph[i], Passed: passed})
	}
	s.render(w, r, http.StatusOK, "campaign", p)
}

func (s *Server) scenario(w http.ResponseWriter, r *http.Request) {
	c, ok := s.lib.Campaign(r.PathValue("campaign"))
	if !ok {
		s.notFound(w, r)
		return
	}
	sc, ok := c.Scenario(r.PathValue("scenario"))
	if !ok {
		s.notFound(w, r)
		return
	}
	completed, available := s.progress(r, c)
	var images []string
	for _, img := range sc.Images {
		images = append(images, "/"+c.Slug+"/"+sc.Slug+"/images/"+path.Base(img))
	}
	inProgress, operationURL, battleCache := false, "", ""
	if seed := currentSeed(r); seed != "" && len(sc.Forces) > 0 && !completed[sc.Slug] {
		if op, ok, err := s.store.MekBayOperation(r.Context(), playthroughID(seed), c.Slug, sc.ID); err != nil {
			log.Printf("load mekbay operation: %v", err)
		} else if ok {
			inProgress = true
			if forces, at, err := s.store.BattleStatusCache(r.Context(), playthroughID(seed), c.Slug, sc.ID); err != nil {
				log.Printf("load the battle status cache: %v", err)
			} else if forces != "" {
				battleCache = `{"forces":` + forces + `,"updatedAt":` + strconv.FormatInt(at, 10) + `}`
			}
			if link, err := s.mekbayLink(r, playthroughID(seed), c.Slug, tableLabel, mekbay.AccountUID(seed, mekbay.SlotHangar), url.Values{"operation": {op.OperationID}}); err != nil {
				log.Printf("link to the operation: %v", err)
			} else {
				operationURL = link
			}
		}
	}
	var next []content.Scenario
	if completed[sc.Slug] {
		for _, b := range sc.Branches {
			if n, ok := c.Scenario(b); ok && available[b] { // a branch not taken (the other choice, once a later mission is done) is not offered
				next = append(next, *n)
			}
		}
	}
	s.render(w, r, http.StatusOK, "scenario", Page{
		InProgress:    inProgress,
		NextScenarios: next,
		OperationURL:  operationURL,
		BattleCache:   battleCache,
		Images:        images,
		Title:         sc.Title + " | " + c.Label,
		Crumbs:        []Crumb{{Label: c.Label, URL: "/" + c.Slug + "/"}, scenarioCrumb(c, sc, available)},
		Campaign:      c,
		Scenario:      sc,
		ScenarioState: scenarioState(sc.Slug, completed, available),
		Outcome:       s.recordedOutcome(r, c, sc, completed),
	})
}

// scenarioCrumb is the breadcrumb for a scenario page, "001 – Pyre Light", with a menu of
// every scenario in the campaign.
func scenarioCrumb(c *content.Campaign, sc *content.Scenario, available map[string]bool) Crumb {
	label := func(s *content.Scenario) string { return s.Number + " – " + s.Title }
	cr := Crumb{Label: label(sc), URL: "/" + c.Slug + "/" + sc.Slug + "/"}
	for i := range c.Scenarios {
		o := &c.Scenarios[i]
		if !available[o.Slug] && o.Slug != sc.Slug {
			continue // locked scenarios stay out of the menu
		}
		cr.Menu = append(cr.Menu, Crumb{Label: label(o), URL: "/" + c.Slug + "/" + o.Slug + "/", Current: o.Slug == sc.Slug})
	}
	return cr
}

// image serves files from a scenario's images/ folder. The scenario is looked up by slug and
// only the base name of the file is used, so URLs can't reach outside that folder.
func (s *Server) image(w http.ResponseWriter, r *http.Request) {
	c, ok := s.lib.Campaign(r.PathValue("campaign"))
	if !ok {
		s.notFound(w, r)
		return
	}
	sc, ok := c.Scenario(r.PathValue("scenario"))
	if !ok {
		s.notFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(sc.Dir, "images", filepath.Base(r.PathValue("file"))))
}

// campaignImage serves files from a campaign's own images/ folder (its icon). Only the base name of the file is used, so URLs can't reach outside that folder.
func (s *Server) campaignImage(w http.ResponseWriter, r *http.Request) {
	c, ok := s.lib.Campaign(r.PathValue("campaign"))
	if !ok {
		s.notFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(c.Dir, "images", filepath.Base(r.PathValue("file"))))
}

// force serves a scenario's .mul force file as a download. Only files listed in the scenario's
// front matter `forces` are served.
func (s *Server) force(w http.ResponseWriter, r *http.Request) {
	c, ok := s.lib.Campaign(r.PathValue("campaign"))
	if !ok {
		s.notFound(w, r)
		return
	}
	sc, ok := c.Scenario(r.PathValue("scenario"))
	if !ok {
		s.notFound(w, r)
		return
	}
	name := r.PathValue("file")
	for _, f := range sc.Forces {
		if f.File == name {
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.Header().Set("Content-Disposition", `attachment; filename="`+f.File+`"`)
			http.ServeFile(w, r, filepath.Join(sc.Dir, f.File))
			return
		}
	}
	s.notFound(w, r)
}
