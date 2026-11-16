package site

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
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
			g.Units = append(g.Units, UnitDraft{Unit: u.Name, BV: u.BV, Pilot: e.Pilot, Gunnery: e.Gunnery, Piloting: e.Piloting})
		}
		d.Groups = append(d.Groups, g)
	}
	return d, nil
}

// createOperation synthesizes the scenario's MekBay operation for the visitor's playthrough: an OpFor force built
// from the scenario's .mul files, paired with their hangar force. It needs modify mode (the owner's UID), because
// MekBay only lets the owner save. Posting again with rebuild=1 resets the OpFor (undoing any damage on it).
func (s *Server) createOperation(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)
		return
	}
	seed := currentSeed(r)
	if seed == "" {
		http.Error(w, "Choose a seed first.", http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	c, ok := s.lib.Campaign(r.PostFormValue("campaign"))
	if !ok {
		http.Error(w, "unknown campaign", http.StatusNotFound)
		return
	}
	sc, ok := c.Scenario(r.PostFormValue("scenario"))
	if !ok || len(sc.Forces) == 0 {
		http.Error(w, "unknown scenario, or it has no OpFor to create", http.StatusNotFound)
		return
	}
	id := playthroughID(seed)
	pt, found, err := s.store.Playthrough(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load playthrough", http.StatusInternalServerError)
		return
	}
	if !found || !pt.Configured() {
		http.Error(w, "Register your MekBay hangar first.", http.StatusBadRequest)
		return
	}
	if !pt.CanModify() {
		http.Error(w, "Add your MekBay user ID in the campaign details to let the site create operations.", http.StatusForbidden)
		return
	}
	existing, has, err := s.store.MekBayOperation(r.Context(), id, sc.ID)
	if err != nil {
		http.Error(w, "could not load operation", http.StatusInternalServerError)
		return
	}
	if has && r.PostFormValue("rebuild") != "1" {
		writeOperation(w, existing)
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
		CampaignInstance: pt.HangarInstance,
		OpFor:            draft,
		OpForInstance:    existing.OpForInstance,
		OperationName:    operationName(c, sc),
		OperationID:      existing.OperationID,
	})
	if err != nil {
		log.Printf("mekbay sync for %s: %v", sc.ID, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	op := MekBayOperation(res)
	if err := s.store.SaveMekBayOperation(r.Context(), id, sc.ID, op); err != nil {
		http.Error(w, "created the operation but could not remember it", http.StatusInternalServerError)
		return
	}
	writeOperation(w, op)
}

func writeOperation(w http.ResponseWriter, op MekBayOperation) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"url": OperationURL(op.OperationID)})
}
