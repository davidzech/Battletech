package site

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Option customizes the handler returned by NewHandler.
type Option func(*Server)

// WithMekBay replaces the MekBay client (tests use a fake one).
func WithMekBay(m MekBay) Option { return func(s *Server) { s.mekbay = m } }

// operationName is the operation's name in MekBay, e.g. "Refusal War - Scenario 001".
func operationName(c *Campaign, sc *Scenario) string { return c.Label + " - Scenario " + sc.Number }

// opForDraft turns a scenario's .mul force files into one OpFor force, a group per file ("OpFor 1", "OpFor 2").
func opForDraft(c *Campaign, sc *Scenario) (ForceDraft, error) {
	d := ForceDraft{Name: operationName(c, sc) + " - OpFor"}
	for _, f := range sc.Forces {
		raw, err := os.ReadFile(filepath.Join(sc.Dir, f.File))
		if err != nil {
			return ForceDraft{}, err
		}
		entities, err := parseMul(raw)
		if err != nil {
			return ForceDraft{}, fmt.Errorf("%s: %w", f.File, err)
		}
		g := GroupDraft{Name: f.Label}
		for _, e := range entities {
			u, ok := lookupUnit(e.Chassis, e.Model)
			if !ok {
				return ForceDraft{}, fmt.Errorf("%s: %s %s is not in the unit catalog", f.File, e.Chassis, e.Model)
			}
			g.Units = append(g.Units, UnitDraft{Unit: u.Name, BV: adjustedBV(u.BV, e.Gunnery, e.Piloting), Pilot: e.Pilot, Gunnery: e.Gunnery, Piloting: e.Piloting})
		}
		d.Groups = append(d.Groups, g)
	}
	return d, nil
}

// hangarUnitView is one hangar unit as the deploy popup shows it.
type hangarUnitView struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`  // the Clan name when the design has one
	Model      string  `json:"model"` // e.g. "Prime"
	Icon       string  `json:"icon"`  // URL of the unit's icon
	Class      string  `json:"class"`
	Role       string  `json:"role"`
	Tons       float64 `json:"tons"`
	BV         int     `json:"bv"` // base battle value; pilot skills never change it in this campaign
	Pilot      string  `json:"pilot"`
	Gunnery    int     `json:"gunnery"`
	Piloting   int     `json:"piloting"`
	Damaged    bool    `json:"damaged"`
	Destroyed  bool    `json:"destroyed"`
	Unknown    bool    `json:"unknown"` // not in the unit catalog, so its BV can't be worked out
	InDeployed bool    `json:"inDeployed"`
}

var chassisNames = regexp.MustCompile(`^(.*?)\s*\((.+)\)$`)

// unitNames returns the name to show for a unit and its model. A design with two names is written "Inner Sphere
// name (Clan name)" in the catalog, and the Clan name always wins: "Vulture (Mad Dog)" is shown as "Mad Dog". Only
// Clan-tech designs follow that pattern; the few Inner Sphere ones with a parenthesis ("Katana (Crockett)") have no
// Clan name, so they keep their first name.
func unitNames(u Unit) (name, model string) {
	name = u.Chassis
	if m := chassisNames.FindStringSubmatch(u.Chassis); m != nil {
		if u.TechBase == "Clan" {
			name = m[2]
		} else {
			name = m[1]
		}
	}
	return name, u.Model
}

// displayName is a unit's name and model on one line, e.g. "Mad Dog Prime".
func displayName(u Unit) string {
	name, model := unitNames(u)
	return strings.TrimSpace(name + " " + model)
}

// hangarViews describes every unit of a hangar for the deploy popup.
func hangarViews(h HangarForce) []hangarUnitView {
	views := make([]hangarUnitView, 0, len(h.Units))
	for _, hu := range h.Units {
		v := hangarUnitView{ID: hu.ID, Pilot: hu.Pilot, Gunnery: hu.Gunnery, Piloting: hu.Piloting, Damaged: hu.Damaged, Destroyed: hu.Destroyed, InDeployed: sameGroupName(hu.Group, deployedGroup)}
		if u, ok := unitByName(hu.Unit); ok {
			v.Name, v.Model = unitNames(u)
			v.Class, v.Role, v.Tons, v.Icon = u.Class, u.Role, u.Tons, iconURL(u)
			v.BV = u.BV // base BV: this campaign ignores pilot skills, so skill can never be a way to game the BV cap
		} else {
			v.Name, v.Unknown, v.Icon = hu.Unit, true, defaultIconURL("")
		}
		views = append(views, v)
	}
	return views
}

// deployProblem checks a chosen set of hangar units against the hangar and the scenario's limits; it returns a
// message for the player, or "" when the choice is fine.
func deployProblem(h HangarForce, ids []string, bvLimit int) string {
	if len(ids) == 0 {
		return "Choose at least one unit to deploy."
	}
	byID := map[string]hangarUnitView{}
	for _, v := range hangarViews(h) {
		byID[v.ID] = v
	}
	seen := map[string]bool{}
	total := 0
	for _, id := range ids {
		v, ok := byID[id]
		switch {
		case seen[id]:
			return "A unit was chosen twice."
		case !ok:
			return "A chosen unit is no longer in your hangar. Reopen the popup to reload it."
		case v.Destroyed:
			return v.Name + " is destroyed and can't be deployed."
		case v.Unknown:
			return v.Name + " isn't in the unit catalog, so its battle value is unknown."
		}
		seen[id] = true
		total += v.BV
	}
	if bvLimit > 0 && total > bvLimit {
		return fmt.Sprintf("The force is %d BV, over this scenario's limit of %d.", total, bvLimit)
	}
	return ""
}

// mekbayRequest is what both MekBay endpoints need first: the seed's playthrough, the campaign and scenario named by
// the request, and the playthrough's run of that campaign (which holds its hangar). It writes the error response
// itself and reports ok=false.
func (s *Server) mekbayRequest(w http.ResponseWriter, r *http.Request) (pt Playthrough, run CampaignRun, c *Campaign, sc *Scenario, ok bool) {
	seed := currentSeed(r)
	if seed == "" {
		http.Error(w, "Choose a seed first.", http.StatusBadRequest)
		return
	}
	c, found := s.lib.Campaign(r.FormValue("campaign"))
	if !found {
		http.Error(w, "unknown campaign", http.StatusNotFound)
		return
	}
	sc, found = c.Scenario(r.FormValue("scenario"))
	if !found || len(sc.Forces) == 0 {
		http.Error(w, "unknown scenario, or it has no OpFor to deploy against", http.StatusNotFound)
		return
	}
	pt, found, err := s.store.Playthrough(r.Context(), playthroughID(seed))
	if err != nil {
		http.Error(w, "could not load playthrough", http.StatusInternalServerError)
		return
	}
	if !found || !pt.Configured() {
		http.Error(w, "Name your company first.", http.StatusBadRequest)
		return
	}
	run, started, err := s.store.Run(r.Context(), pt.ID, c.Slug)
	if err != nil {
		http.Error(w, "could not load campaign", http.StatusInternalServerError)
		return
	}
	if !started {
		http.Error(w, "Start this campaign first.", http.StatusBadRequest)
		return
	}
	pilots, err := s.store.Pilots(r.Context(), pt.ID, c.Slug)
	if err != nil {
		http.Error(w, "could not load named pilots", http.StatusInternalServerError)
		return
	}
	if _, alive := pilotViews(pilots); alive < c.StartingNamedPilots {
		http.Error(w, fmt.Sprintf("Name your %d starting named pilots on the campaign page first.", c.StartingNamedPilots), http.StatusBadRequest)
		return
	}
	return pt, run, c, sc, true
}

// hangarJSON lists the player's hangar for the deploy popup, with the scenario's BV limit.
func (s *Server) hangarJSON(w http.ResponseWriter, r *http.Request) {
	pt, run, c, sc, ok := s.mekbayRequest(w, r)
	if !ok {
		return
	}
	h, err := s.mekbay.ReadHangar(r.Context(), run.HangarInstance)
	if err != nil {
		log.Printf("read hangar for %s: %v", sc.ID, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	op, hasOp, err := s.store.MekBayOperation(r.Context(), pt.ID, c.Slug, sc.ID)
	if err != nil {
		http.Error(w, "could not load operation", http.StatusInternalServerError)
		return
	}
	out := map[string]any{"name": h.Name, "limit": sc.BVLimit, "canModify": pt.CanModify(), "units": hangarViews(h), "hasOperation": hasOp}
	if hasOp {
		out["url"] = OperationURL(op.OperationID)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}

// deploy sorts the player's hangar into "Deployed" (the chosen units) and "Hangar" (the rest) in MekBay, creates the
// scenario's OpFor force and ties both into one operation. It needs modify mode (the owner's UID), because MekBay only
// lets the owner save. An existing OpFor is kept, with whatever damage it has, unless reset_opfor=1.
func (s *Server) deploy(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	pt, run, c, sc, ok := s.mekbayRequest(w, r)
	if !ok {
		return
	}
	if !pt.CanModify() {
		http.Error(w, "Add your MekBay user ID in the campaign details to let the site deploy for you.", http.StatusForbidden)
		return
	}
	ids := r.PostForm["units"]
	h, err := s.mekbay.ReadHangar(r.Context(), run.HangarInstance)
	if err != nil {
		log.Printf("read hangar for %s: %v", sc.ID, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if msg := deployProblem(h, ids, sc.BVLimit); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	existing, has, err := s.store.MekBayOperation(r.Context(), pt.ID, c.Slug, sc.ID)
	if err != nil {
		http.Error(w, "could not load operation", http.StatusInternalServerError)
		return
	}
	draft, err := opForDraft(c, sc)
	if err != nil {
		log.Printf("opfor draft for %s: %v", sc.ID, err) // never log the owner UID
		http.Error(w, "could not read this scenario's OpFor files", http.StatusInternalServerError)
		return
	}
	res, err := s.mekbay.SyncOperation(r.Context(), OperationRequest{
		OwnerUID:         pt.OwnerUID,
		CampaignInstance: run.HangarInstance,
		DeployedUnitIDs:  ids,
		OpFor:            draft,
		OpForInstance:    existing.OpForInstance,
		KeepOpFor:        has && r.PostFormValue("reset_opfor") != "1",
		OperationName:    operationName(c, sc),
		OperationID:      existing.OperationID,
	})
	if err != nil {
		log.Printf("mekbay sync for %s: %v", sc.ID, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	op := MekBayOperation(res)
	if err := s.store.SaveMekBayOperation(r.Context(), pt.ID, c.Slug, sc.ID, op); err != nil {
		http.Error(w, "deployed in MekBay but could not remember it", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"url": OperationURL(op.OperationID)})
}
