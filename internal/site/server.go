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

// Crumb is one breadcrumb link.
type Crumb struct{ Label, URL string }

// Page is the data every template receives.
type Page struct {
	Title    string
	Crumbs   []Crumb
	Campaign *Campaign
	Scenario *Scenario
	Next     *Scenario
	Cards    []Card
	Images   []string // URLs of the scenario's images
}

// Card is one entry in a list of links (campaigns or scenarios).
type Card struct{ Num, Name, URL string }

// Server serves the campaigns found under the content root.
type Server struct {
	lib   Library
	pages map[string]*template.Template
}

// NewHandler returns the site's HTTP handler. Campaign files are re-read on every request,
// so edits show up on refresh.
func NewHandler(contentRoot string) http.Handler {
	s := &Server{lib: Library{Root: contentRoot}, pages: map[string]*template.Template{}}
	for _, name := range []string{"home", "campaign", "scenario", "keywords", "notfound"} {
		s.pages[name] = template.Must(template.ParseFS(assets, "templates/layout.html", "templates/"+name+".html"))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /static/{file}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, assets, path.Join("static", path.Base(r.PathValue("file"))))
	})
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /{campaign}/{$}", s.campaign)
	mux.HandleFunc("GET /{campaign}/keywords/{$}", s.keywords)
	mux.HandleFunc("GET /{campaign}/{scenario}/{$}", s.scenario)
	mux.HandleFunc("GET /{campaign}/{scenario}/images/{file}", s.image)
	mux.HandleFunc("GET /{campaign}/{scenario}/forces/{file}", s.force)
	mux.HandleFunc("/", s.notFound)
	return mux
}

func (s *Server) render(w http.ResponseWriter, status int, name string, p Page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.pages[name].ExecuteTemplate(w, "layout", p); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusNotFound, "notfound", Page{Title: "Not found | BattleTech Campaigns"})
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	cs, err := s.lib.Campaigns()
	if err != nil {
		http.Error(w, "cannot read campaigns", http.StatusInternalServerError)
		log.Print(err)
		return
	}
	p := Page{Title: "BattleTech Campaigns"}
	for _, c := range cs {
		p.Cards = append(p.Cards, Card{Num: strconv.Itoa(len(c.Scenarios)), Name: c.Label, URL: "/" + c.Slug + "/"})
	}
	s.render(w, http.StatusOK, "home", p)
}

func (s *Server) campaign(w http.ResponseWriter, r *http.Request) {
	c, ok := s.lib.Campaign(r.PathValue("campaign"))
	if !ok {
		s.notFound(w, r)
		return
	}
	p := Page{
		Title:    c.Label + " | BattleTech Campaigns",
		Crumbs:   []Crumb{{c.Label, "/" + c.Slug + "/"}},
		Campaign: c,
	}
	for _, sc := range c.Scenarios {
		p.Cards = append(p.Cards, Card{Num: sc.Number, Name: sc.Title, URL: "/" + c.Slug + "/" + sc.Slug + "/"})
	}
	s.render(w, http.StatusOK, "campaign", p)
}

func (s *Server) keywords(w http.ResponseWriter, r *http.Request) {
	c, ok := s.lib.Campaign(r.PathValue("campaign"))
	if !ok {
		s.notFound(w, r)
		return
	}
	s.render(w, http.StatusOK, "keywords", Page{
		Title:    "Keywords | " + c.Label,
		Crumbs:   []Crumb{{c.Label, "/" + c.Slug + "/"}, {"Keywords", "/" + c.Slug + "/keywords/"}},
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
	s.render(w, http.StatusOK, "scenario", Page{
		Images:   images,
		Title:    sc.Title + " | " + c.Label,
		Crumbs:   []Crumb{{c.Label, "/" + c.Slug + "/"}, {"Scenario " + sc.Number, "/" + c.Slug + "/" + sc.Slug + "/"}},
		Campaign: c,
		Scenario: sc,
		Next:     next,
	})
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
