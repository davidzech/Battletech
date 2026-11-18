package site

import (
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
	// BackURL and BackLabel, when set, replace the Manage page's "Back to the campaign" link (it returns to the sortie the player came from).
	BackURL, BackLabel string
	// MekBayEditURL, on the Manage page, opens the site's hangar copy in the MekBay proxy acting as the account that owns it, so it is editable.
	MekBayEditURL string
	Crumbs        []Crumb
	Campaign      *Campaign
	OnManagePage  bool // the Manage force page itself, which has no Manage force button in its header
	Scenario      *Scenario
	Cards         []Card
	HasLocked     bool           // some scenario cards are locked, so the campaign page offers 'Show all scenarios'
	ScenarioState string         // on a scenario page: completed, available or locked
	NextScenarios []Scenario     // on a completed scenario's page: the scenarios it leads to, which are now available
	Ledger        []LedgerEntry  // the keywords the campaign has been granted (on the keywords page)
	Outcome       *OutcomeResult // on a completed scenario page: the recorded report of its outcome
	Images        []string       // URLs of the scenario's images
	Seed          string         // the visitor's seed ("" if none chosen yet)
	Play          Playthrough    // the visitor's playthrough (zero value if no seed)
	Run           CampaignRun    // the playthrough's run of this page's campaign (zero value until it is started)
	Pilots        []PilotView    // the run's named pilots
	CanRecruit    bool           // the command has room for another living named pilot, and the warchest can pay for them
	RecruitNote   string         // the short gray note under Recruit New Pilot: why it is unavailable, or what it costs
	MaxPilots     int            // most living named pilots a command may have
	// CachedHangar is the hangar last read from MekBay for this run (nil if none yet), and CachedAt when; the campaign and scenario
	// pages embed it so they can paint the hangar before the live read finishes (see hangarcache.go).
	CachedHangar *HangarForce
	CachedAt     time.Time
	Ranks        []string         // the campaign's pilot ranks, lowest first (empty when it has none)
	Memorial     bool             // at least one named pilot was killed in action
	Ladders      string           // the pilot improvement ladders as JSON, for the pilot builder's live preview
	Abilities    string           // the Edge ability catalog as JSON, for the ability picker
	Portraits    string           // the portrait list as JSON, for the portrait picker
	InProgress   bool             // the scenario has a battle in progress (an operation that is neither completed nor aborted)
	OperationURL string           // the table's link to that battle, so the Operation in progress view can show it before anything is read from MekBay
	Checkpoints  []CheckpointView // the campaign's rollback points, newest first (the Manage page)
	BattleCache  string           // the last status read of that battle as JSON ({forces, updatedAt}), so the view paints it at once while a fresh read is made
	Commit       string           // git commit the server was built from, short form ("dev" if unknown)
}

// Card is one entry in a list of links (campaigns or scenarios).
type Card struct{ Num, Name, Sub, URL, State string } // State: "completed", "available" or "locked" for a scenario card

// Server serves the campaigns found under the content root.
type Server struct {
	lib    Library
	store  *Store
	pages  map[string]*template.Template
	mekbay MekBay

	// The MekBay proxy (mekbayproxy.go), served on the hosts mekbay-<link id>.<site host> (and mekbay.localhost).
	linkKey  string
	upstream *[3]string // web, WebSocket and Origin of the MekBay to proxy; tests replace it
	proxy    *mekbayProxy
	onProxy  func(*mekbayProxy) // tests take hold of the proxy here
}

// NewHandler returns the site's HTTP handler. Campaign files are re-read on every request,
// so edits show up on refresh.
func NewHandler(contentRoot string, store *Store, opts ...Option) http.Handler {
	s := &Server{lib: Library{Root: contentRoot}, store: store, pages: map[string]*template.Template{}, mekbay: &MekBayClient{}}
	for _, o := range opts {
		o(s)
	}
	s.proxy = newMekBayProxy(newMekBayVault(s.linkKey), store)
	if s.upstream != nil {
		s.proxy.web, s.proxy.ws, s.proxy.origin = s.upstream[0], s.upstream[1], s.upstream[2]
	}
	if s.onProxy != nil {
		s.onProxy(s.proxy)
	}
	for _, name := range []string{"home", "campaign", "manage", "scenario", "keywords", "notfound"} {
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
	mux.HandleFunc("POST /api/seed", s.setSeed)
	mux.HandleFunc("POST /api/account", s.setAccount)
	mux.HandleFunc("POST /api/campaign/start", s.startCampaign)
	mux.HandleFunc("POST /api/campaign/delete", s.deleteCampaign)
	mux.HandleFunc("GET /api/campaign/hangar-check", s.hangarCheck)
	mux.HandleFunc("GET /api/campaign/force", s.forceJSON)
	mux.HandleFunc("POST /api/pilots", s.addPilot)
	mux.HandleFunc("POST /api/pilots/sp", s.awardSP)
	mux.HandleFunc("POST /api/pilots/earn", s.earnSP)
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
	mux.HandleFunc("POST /api/mekbay/deploy", s.deploy)
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /{campaign}/{$}", s.campaign)
	mux.HandleFunc("GET /{campaign}/keywords/{$}", s.keywords)
	mux.HandleFunc("GET /{campaign}/manage/{$}", s.manage)
	mux.HandleFunc("GET /{campaign}/{scenario}/{$}", s.scenario)
	mux.HandleFunc("GET /{campaign}/{scenario}/images/{file}", s.image)
	mux.HandleFunc("GET /{campaign}/{scenario}/forces/{file}", s.force)
	mux.HandleFunc("/", s.notFound)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isMekBayHost(r.Host) {
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
				p.Ladders, p.Portraits, p.Abilities = laddersJSON, portraitsJSON(), edgeAbilitiesJSON
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
					p.Pilots, alive = pilotViews(pilots, p.Campaign.Ranks)
					limit := p.Campaign.PilotLimit()
					cost := p.Campaign.RecruitCost
					free := p.Run.FreeRecruits > 0
					p.CanRecruit = alive < limit && (free || p.Run.Warchest >= cost)
					switch {
					case alive >= limit:
						p.RecruitNote = fmt.Sprintf("Your command is full: %d of %d pilots.", alive, limit)
					case free:
						p.RecruitNote = "A replacement pilot is free: recruiting costs no SP."
					case p.Run.Warchest < cost:
						p.RecruitNote = fmt.Sprintf("Recruiting costs %s SP (warchest: %s).", withCommas(cost), withCommas(p.Run.Warchest))
					case cost > 0:
						p.RecruitNote = fmt.Sprintf("Recruiting costs %s SP.", withCommas(cost))
					}
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
					p.MaxPilots = limit
				}
			}
		}
	}
	p.Commit = shortCommit(buildCommit)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.pages[name].ExecuteTemplate(w, "layout", p); err != nil {
		log.Printf("render %s: %v", name, err)
	}
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
		p.Cards = append(p.Cards, Card{Name: c.Label, Sub: c.PlayersLabel(), URL: "/" + c.Slug + "/"})
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
	for _, sc := range c.Scenarios {
		state := scenarioState(sc.Slug, completed, available)
		p.HasLocked = p.HasLocked || state == "locked"
		p.Cards = append(p.Cards, Card{Num: sc.Number, Name: sc.Title, Sub: sc.Planet, URL: "/" + c.Slug + "/" + sc.Slug + "/", State: state})
	}
	s.render(w, r, http.StatusOK, "campaign", p)
}

func (s *Server) keywords(w http.ResponseWriter, r *http.Request) {
	c, ok := s.lib.Campaign(r.PathValue("campaign"))
	if !ok {
		s.notFound(w, r)
		return
	}
	p := Page{
		Title:    "Keywords | " + c.Label,
		Crumbs:   []Crumb{{Label: c.Label, URL: "/" + c.Slug + "/"}, {Label: "Keywords", URL: "/" + c.Slug + "/keywords/"}},
		Campaign: c,
	}
	if seed := currentSeed(r); seed != "" {
		if ledger, err := s.store.Ledger(r.Context(), playthroughID(seed), c.Slug); err != nil {
			log.Printf("load the campaign ledger: %v", err)
		} else {
			p.Ledger = ledger
		}
	}
	s.render(w, r, http.StatusOK, "keywords", p)
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
			if link, err := s.mekbayLink(r, playthroughID(seed), c.Slug, tableLabel, accountUID(seed, SlotHangar), url.Values{"operation": {op.OperationID}}); err != nil {
				log.Printf("link to the operation: %v", err)
			} else {
				operationURL = link
			}
		}
	}
	var next []Scenario
	if completed[sc.Slug] {
		for _, b := range sc.Branches {
			if n, ok := c.Scenario(b); ok {
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
func scenarioCrumb(c *Campaign, sc *Scenario, available map[string]bool) Crumb {
	label := func(s *Scenario) string { return s.Number + " – " + s.Title }
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
