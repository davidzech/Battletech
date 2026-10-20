package site

import (
	"embed"
	"html/template"
	"log"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
)

//go:embed templates/*.html static/*
var assets embed.FS

// Crumb is one breadcrumb link. A crumb with a Menu opens a dropdown of sibling pages
// (the campaign's scenarios) instead of linking straight to URL; Current marks the open page.
type Crumb struct {
	Label, URL string
	Current    bool
	Menu       []Crumb
}

// Page is the data every template receives.
type Page struct {
	Title    string
	Crumbs   []Crumb
	Campaign *Campaign
	Scenario *Scenario
	Next     *Scenario
	Cards    []Card
	Images   []string    // URLs of the scenario's images
	Seed     string      // the visitor's seed ("" if none chosen yet)
	SeedHint string      // short form of Seed for the header
	Play     Playthrough // the visitor's playthrough (zero value if no seed)
}

// Card is one entry in a list of links (campaigns or scenarios).
type Card struct{ Num, Name, Sub, URL string }

// Server serves the campaigns found under the content root.
type Server struct {
	lib   Library
	store *Store
	pages map[string]*template.Template
}

// NewHandler returns the site's HTTP handler. Campaign files are re-read on every request,
// so edits show up on refresh.
func NewHandler(contentRoot string, store *Store) http.Handler {
	s := &Server{lib: Library{Root: contentRoot}, store: store, pages: map[string]*template.Template{}}
	for _, name := range []string{"home", "campaign", "scenario", "keywords", "notfound"} {
		s.pages[name] = template.Must(template.ParseFS(assets, "templates/layout.html", "templates/"+name+".html"))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /static/{file}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, assets, path.Join("static", path.Base(r.PathValue("file"))))
	})
	mux.HandleFunc("POST /api/seed", s.setSeed)
	mux.HandleFunc("POST /api/campaign", s.setCampaign)
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /{campaign}/{$}", s.campaign)
	mux.HandleFunc("GET /{campaign}/keywords/{$}", s.keywords)
	mux.HandleFunc("GET /{campaign}/{scenario}/{$}", s.scenario)
	mux.HandleFunc("GET /{campaign}/{scenario}/images/{file}", s.image)
	mux.HandleFunc("GET /{campaign}/{scenario}/forces/{file}", s.force)
	mux.HandleFunc("/", s.notFound)
	return mux
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, name string, p Page) {
	if seed := currentSeed(r); seed != "" {
		p.Seed, p.SeedHint = seed, seedHint(seed)
		id := playthroughID(seed)
		if _, err := s.store.EnsurePlaythrough(r.Context(), id); err != nil {
			log.Printf("ensure playthrough: %v", err)
		} else if pt, _, err := s.store.Playthrough(r.Context(), id); err != nil {
			log.Printf("load playthrough: %v", err)
		} else {
			p.Play = pt
		}
	}
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
		p.Cards = append(p.Cards, Card{Num: strconv.Itoa(len(c.Scenarios)), Name: c.Label, URL: "/" + c.Slug + "/"})
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
	s.render(w, r, http.StatusOK, "scenario", Page{
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
