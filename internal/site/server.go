package site

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"html/template"
	"log"
	"net/http"
	"path"
	"path/filepath"
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
	Crumbs             []Crumb
	Campaign           *Campaign
	Scenario           *Scenario
	Next               *Scenario
	Cards              []Card
	Images             []string    // URLs of the scenario's images
	Seed               string      // the visitor's seed ("" if none chosen yet)
	Play               Playthrough // the visitor's playthrough (zero value if no seed)
	Run                CampaignRun // the playthrough's run of this page's campaign (zero value until it is started)
	Pilots             []PilotView // the run's named pilots
	CanRecruit         bool        // the command has room for another living named pilot
	MaxPilots          int         // most living named pilots a command may have
	// CachedHangar is the hangar last read from MekBay for this run (nil if none yet), and CachedAt when; the campaign and scenario
	// pages embed it so they can paint the hangar before the live read finishes (see hangarcache.go).
	CachedHangar *HangarForce
	CachedAt     time.Time
	Ranks        []string // the campaign's pilot ranks, lowest first (empty when it has none)
	Memorial     bool     // at least one named pilot was killed in action
	Ladders      string   // the pilot improvement ladders as JSON, for the pilot builder's live preview
	Abilities    string   // the Edge ability catalog as JSON, for the ability picker
	Portraits    string   // the portrait list as JSON, for the portrait picker
	MekBayOp     string   // link to the scenario's MekBay operation, once created ("" until then)
	Commit       string   // git commit the server was built from, short form ("dev" if unknown)
}

// Card is one entry in a list of links (campaigns or scenarios).
type Card struct{ Num, Name, Sub, URL string }

// Server serves the campaigns found under the content root.
type Server struct {
	lib    Library
	store  *Store
	pages  map[string]*template.Template
	mekbay MekBay
}

// NewHandler returns the site's HTTP handler. Campaign files are re-read on every request,
// so edits show up on refresh.
func NewHandler(contentRoot string, store *Store, opts ...Option) http.Handler {
	s := &Server{lib: Library{Root: contentRoot}, store: store, pages: map[string]*template.Template{}, mekbay: &MekBayClient{}}
	for _, o := range opts {
		o(s)
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
	mux.HandleFunc("POST /api/campaign/force-name", s.setForceName)
	mux.HandleFunc("POST /api/hangar/service", s.serviceUnit)
	mux.HandleFunc("POST /api/hangar/purchase", s.purchaseUnits)
	mux.HandleFunc("POST /api/hangar/sell", s.sellUnit)
	mux.HandleFunc("POST /api/hangar/swap", s.swapUnit)
	mux.HandleFunc("POST /api/deploy/plan", s.deployPlan)
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
	return mux
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
					p.CanRecruit = alive < maxNamedPilots
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
					p.MaxPilots = maxNamedPilots
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
	for _, sc := range c.Scenarios {
		p.Cards = append(p.Cards, Card{Num: sc.Number, Name: sc.Title, Sub: sc.Planet, URL: "/" + c.Slug + "/" + sc.Slug + "/"})
	}
	s.render(w, r, http.StatusOK, "campaign", p)
}

func (s *Server) keywords(w http.ResponseWriter, r *http.Request) {
	c, ok := s.lib.Campaign(r.PathValue("campaign"))
	if !ok {
		s.notFound(w, r)
		return
	}
	s.render(w, r, http.StatusOK, "keywords", Page{
		Title:    "Keywords | " + c.Label,
		Crumbs:   []Crumb{{Label: c.Label, URL: "/" + c.Slug + "/"}, {Label: "Keywords", URL: "/" + c.Slug + "/keywords/"}},
		Campaign: c,
	})
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
	next, _ := c.ScenarioByID(sc.Next)
	var images []string
	for _, img := range sc.Images {
		images = append(images, "/"+c.Slug+"/"+sc.Slug+"/images/"+path.Base(img))
	}
	var opLink string
	if seed := currentSeed(r); seed != "" && len(sc.Forces) > 0 {
		if op, ok, err := s.store.MekBayOperation(r.Context(), playthroughID(seed), c.Slug, sc.ID); err != nil {
			log.Printf("load mekbay operation: %v", err)
		} else if ok {
			opLink = OperationURL(op.OperationID)
		}
	}
	s.render(w, r, http.StatusOK, "scenario", Page{
		MekBayOp: opLink,
		Images:   images,
		Title:    sc.Title + " | " + c.Label,
		Crumbs:   []Crumb{{Label: c.Label, URL: "/" + c.Slug + "/"}, scenarioCrumb(c, sc)},
		Campaign: c,
		Scenario: sc,
		Next:     next,
	})
}

// scenarioCrumb is the breadcrumb for a scenario page, "001 – Pyre Light", with a menu of
// every scenario in the campaign.
func scenarioCrumb(c *Campaign, sc *Scenario) Crumb {
	label := func(s *Scenario) string { return s.Number + " – " + s.Title }
	cr := Crumb{Label: label(sc), URL: "/" + c.Slug + "/" + sc.Slug + "/"}
	for i := range c.Scenarios {
		o := &c.Scenarios[i]
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
