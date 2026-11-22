package site

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/davidzech/Battletech/internal/storage"

	"github.com/davidzech/Battletech/internal/mekbay"
)

// battleResponse is what Start battle answers: the one link the whole table opens.
type battleResponse struct {
	URL       string `json:"url"`
	Operation string `json:"operation"`
}

// playerDraft is a player's force built from scratch, for a scenario whose mechs are provided and so are not in MekBay: the units they took, with the crews
// they were given.
func playerDraft(name string, h mekbay.HangarForce, units []string, crews []mekbay.CrewAssignment, views map[string]hangarUnitView) mekbay.ForceDraft {
	byID := map[string]mekbay.HangarUnit{}
	for _, u := range h.Units {
		byID[u.ID] = u
	}
	crew := map[string]mekbay.CrewAssignment{}
	for _, a := range crews {
		crew[a.UnitID] = a
	}
	g := mekbay.GroupDraft{Name: "Deployed"}
	for _, id := range units {
		u := byID[id]
		ud := mekbay.UnitDraft{Unit: u.Unit, BV: views[id].BV, Pilot: u.Pilot, Gunnery: u.Gunnery, Piloting: u.Piloting}
		if a, ok := crew[id]; ok {
			ud.Pilot, ud.Gunnery, ud.Piloting = a.Name, a.Gunnery, a.Piloting
		}
		if ud.Pilot == "" {
			ud.Gunnery, ud.Piloting = mekbay.DefaultGunnery, mekbay.DefaultPiloting
		}
		g.Units = append(g.Units, ud)
	}
	return mekbay.ForceDraft{Name: name, Groups: []mekbay.GroupDraft{g}}
}

// startBattle is POST /api/battle/start: the Deploy wizard's last step. It takes the same fields as POST /api/deploy/plan (and checks them the same way), plus
// reset_opfor when the OpFor of an earlier start should be rebuilt instead of kept. It then builds a force for each player and one for the OpFor in MekBay, saves
// the operation that pits them against each other (see battle.go), records the scenario as started and answers with the one link the whole table opens.
// Starting again updates the same operation, so the link stays the same.
func (s *Server) startBattle(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	bp, ok := s.checkPlan(w, r)
	if !ok {
		return
	}
	seed := currentSeed(r)
	account := mekbay.AccountUID(seed, mekbay.SlotHangar)
	draft, err := opForDraft(bp.c, bp.sc)
	if err != nil {
		log.Printf("build the OpFor for %s: %v", bp.sc.ID, err)
		http.Error(w, "could not read the scenario's OpFor", http.StatusInternalServerError)
		return
	}
	// A repeated start updates the same operation, and keeps the OpFor (and the damage recorded on it) unless asked to rebuild it.
	req := mekbay.BattleRequest{
		Account:        account,
		HangarInstance: bp.run.HangarInstance,
		Provided:       bp.sc.Provides(),
		OpFor:          mekbay.BattleOpFor{Draft: draft},
		OperationName:  operationName(bp.c, bp.sc),
	}
	if prev, had, err := s.store.MekBayOperation(r.Context(), bp.pt.ID, bp.c.Slug, bp.sc.ID); err != nil {
		http.Error(w, "could not load the battle", http.StatusInternalServerError)
		return
	} else if had {
		req.OperationID = prev.OperationID
		if r.PostFormValue("reset_opfor") == "" {
			req.OpFor.Reuse = prev.OpForInstance
		}
	}
	for i := range bp.units {
		num := i + 1
		name := fmt.Sprintf("%s - Player %d", bp.run.ForceLabel(), num)
		p := mekbay.BattlePlayer{Number: num, Name: name, Units: bp.units[i], Crew: bp.crews[i]}
		if req.Provided {
			p.Draft = playerDraft(name, bp.h, bp.units[i], bp.crews[i], bp.views)
		}
		req.Players = append(req.Players, p)
	}
	// Building the battle is not abandoned if the browser goes away part-way.
	got, err := s.mekbay.StartBattle(context.WithoutCancel(r.Context()), req)
	if err != nil {
		log.Printf("start battle %s: %v", bp.sc.ID, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if err := s.store.SaveMekBayOperation(r.Context(), bp.pt.ID, bp.c.Slug, bp.sc.ID, storage.MekBayOperation{OpForInstance: got.OpForInstance, OperationID: got.OperationID}); err != nil {
		log.Printf("record operation: %v", err)
		http.Error(w, "The battle was started in MekBay but could not be recorded here.", http.StatusInternalServerError)
		return
	}
	link, err := s.mekbayLink(r, bp.pt.ID, bp.c.Slug, tableLabel, account, url.Values{"operation": {got.OperationID}})
	if err != nil {
		log.Printf("link to the operation: %v", err)
		http.Error(w, "could not make the link", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(battleResponse{URL: link, Operation: got.OperationID})
}

// statusForce is one force of a battle's status, with its mechs described as the Hangar rows describe them (condition, ammunition and so on).
type statusForce struct {
	Name      string           `json:"name"`
	Alignment string           `json:"alignment"` // always "friendly": the page shows the players' forces, not the OpFor's
	Units     []hangarUnitView `json:"units"`
}

// battleStatusResponse is how a battle in progress stands: the link the table opens and every force with its mechs' live condition.
type battleStatusResponse struct {
	URL       string        `json:"url"`
	Operation string        `json:"operation"`
	Forces    []statusForce `json:"forces"`
	UpdatedAt int64         `json:"updatedAt"` // Unix time the status was read from MekBay
}

// battleStatus is GET /api/battle/status: read the scenario's operation and its forces from MekBay and say how the battle stands. Damage, ammunition and crews are
// whatever the players have recorded in MekBay, so the page asks again every few seconds.
func (s *Server) battleStatus(w http.ResponseWriter, r *http.Request) {
	pt, _, c, sc, ok := s.mekbayRequest(w, r)
	if !ok {
		return
	}
	op, had, err := s.store.MekBayOperation(r.Context(), pt.ID, c.Slug, sc.ID)
	if err != nil {
		http.Error(w, "could not load the battle", http.StatusInternalServerError)
		return
	}
	if !had {
		http.Error(w, "There is no battle in progress.", http.StatusNotFound)
		return
	}
	got, err := s.mekbay.ReadBattle(r.Context(), op.OperationID)
	if err != nil {
		log.Printf("read battle %s: %v", sc.ID, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	link, err := s.mekbayLink(r, pt.ID, c.Slug, tableLabel, mekbay.AccountUID(currentSeed(r), mekbay.SlotHangar), url.Values{"operation": {op.OperationID}})
	if err != nil {
		http.Error(w, "could not make the link", http.StatusInternalServerError)
		return
	}
	out := battleStatusResponse{URL: link, Operation: op.OperationID, Forces: []statusForce{}, UpdatedAt: time.Now().Unix()}
	for _, f := range got.Forces {
		out.Forces = append(out.Forces, statusForce{Name: f.Force.Name, Alignment: f.Alignment, Units: hangarViews(f.Force)})
	}
	if b, err := json.Marshal(out.Forces); err == nil { // kept so the next visit paints at once (the link is never stored: it is built from the request)
		if err := s.store.SaveBattleStatus(r.Context(), pt.ID, c.Slug, sc.ID, string(b), out.UpdatedAt); err != nil {
			log.Printf("keep the battle status: %v", err)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}

// abortBattle is POST /api/battle/abort: give the battle up. The scenario goes back to not started, with no rewards; its forces stay in MekBay, which is told nothing.
func (s *Server) abortBattle(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)
		return
	}
	pt, _, c, sc, ok := s.mekbayRequest(w, r)
	if !ok {
		return
	}
	op, had, err := s.store.MekBayOperation(r.Context(), pt.ID, c.Slug, sc.ID)
	if err != nil {
		http.Error(w, "could not abort the battle", http.StatusInternalServerError)
		return
	} else if !had {
		http.Error(w, "There is no battle in progress.", http.StatusNotFound)
		return
	}
	// Two ways to give a battle up (the `mode` field): "keep" (the default) closes it and cleans up its operation and forces in MekBay, keeping every change made
	// since the last checkpoint (repairs, sales, purchases, pilots); "rollback" also puts the campaign back to that checkpoint (the end of the previous mission),
	// undoing them. A campaign with no checkpoint (started before they existed) can only keep.
	mode := r.PostFormValue("mode")
	if mode != "" && mode != "keep" && mode != "rollback" {
		http.Error(w, "Choose how to abort: keep your changes or roll back.", http.StatusBadRequest)
		return
	}
	defer s.deleteBattle(r.Context(), currentSeed(r), op.OperationID)
	if cps, err := s.store.Checkpoints(r.Context(), pt.ID, c.Slug); err == nil && len(cps) > 0 && mode == "rollback" {
		if err := s.rollbackTo(r.Context(), currentSeed(r), pt.ID, c.Slug, cps[0].Seq); err != nil {
			log.Printf("roll back on abort: %v", err)
			http.Error(w, "Could not roll the campaign back: "+err.Error(), http.StatusBadGateway)
			return
		}
	}
	if _, err := s.store.DeleteMekBayOperation(r.Context(), pt.ID, c.Slug, sc.ID); err != nil {
		http.Error(w, "could not abort the battle", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteBattle cleans up an aborted battle in MekBay (its operation and every force in it), in the background of the request: a failure only leaves them behind.
func (s *Server) deleteBattle(ctx context.Context, seed, operationID string) {
	if err := s.mekbay.DeleteBattle(context.WithoutCancel(ctx), mekbay.AccountUID(seed, mekbay.SlotHangar), operationID); err != nil {
		log.Printf("delete the aborted battle in MekBay: %v", err)
	}
}
