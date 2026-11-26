package site

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/storage"
)

// battleResponse is what Start battle answers: the one link the whole table opens.
type battleResponse struct {
	URL       string `json:"url"`
	Operation string `json:"operation"`
}

// playerDraft is a player's force built from scratch, for a scenario whose mechs are provided and so are not in MekBay: the units they took, with the crews
// they were given.
func playerDraft(name string, h mekbay.HangarForce, units []string, crews []mekbay.CrewAssignment, views map[string]hangarUnitView, commander string) mekbay.ForceDraft {
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
		ud := mekbay.UnitDraft{Unit: u.Unit, BV: views[id].BV, Pilot: u.Pilot, Gunnery: u.Gunnery, Piloting: u.Piloting, Commander: id == commander}
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

// startBattle is POST /api/battle/start: the Deploy wizard's last step. It takes the same fields as POST /api/deploy/plan (and checks them the same way). It
// then builds a force for each player and one for the OpFor in MekBay, saves the operation that pits them against each other (see battle.go), records the
// scenario as started and answers with the one link the whole table opens. There is one battle at a time: a campaign with a battle in progress refuses a
// start (it is completed or aborted first), and the start claims the campaign while it builds (Store.ClaimBattle), so two starts at once can't both go ahead.
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
	// One battle at a time, and a battle in progress is finished (completed or aborted) before it can be started again. This is checked first, so a refused
	// start never reads the hangar from MekBay.
	if pt, c, _, ok := s.openRunRequest(w, r); !ok || !s.battleLock(w, r, pt, c, "") {
		return
	}
	bp, ok := s.checkPlan(w, r)
	if !ok {
		return
	}
	// A locked mission can't be fought: the page disables its Deploy force button, and this refuses a request made without the page.
	if _, available := s.progress(r, bp.c); !available[bp.sc.Slug] {
		http.Error(w, "That mission isn't available: it doesn't follow your last completed mission.", http.StatusConflict)
		return
	}
	// Claim the campaign while the battle is built, then look again: a start that finished between the check above and the claim has recorded its battle by
	// now (it records before it releases its claim). While the claim holds, every other change to the campaign is refused too (battleLock sees it).
	ctx := context.WithoutCancel(r.Context()) // building the battle is not abandoned if the browser goes away part-way
	if claimed, err := s.store.ClaimBattle(ctx, bp.pt.ID, bp.c.Slug, bp.sc.ID); err != nil {
		http.Error(w, "could not claim the campaign", http.StatusInternalServerError)
		return
	} else if !claimed {
		if !s.battleLock(w, r, bp.pt, bp.c, "") {
			return
		}
		http.Error(w, "Another battle is being started. Try again in a moment.", http.StatusConflict)
		return
	}
	defer func() {
		if err := s.store.ReleaseBattleClaim(ctx, bp.pt.ID, bp.c.Slug); err != nil {
			log.Printf("release the battle claim: %v", err)
		}
	}()
	if sc, err := s.battleInProgress(ctx, bp.pt.ID, bp.c, false); err != nil {
		http.Error(w, "could not load the battle", http.StatusInternalServerError)
		return
	} else if sc != nil {
		http.Error(w, battleLockMessage(bp.c, sc), http.StatusConflict)
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
	allies, err := alliesDraft(bp.c, bp.sc)
	if err != nil {
		log.Printf("build the allies for %s: %v", bp.sc.ID, err)
		http.Error(w, "could not read the scenario's allies", http.StatusInternalServerError)
		return
	}
	req := mekbay.BattleRequest{
		Allies:         allies,
		Account:        account,
		HangarInstance: bp.run.HangarInstance,
		Provided:       bp.sc.Provides(),
		OpFor:          mekbay.BattleOpFor{Draft: draft},
		OperationName:  operationName(bp.c, bp.sc),
	}
	for i := range bp.units {
		num := i + 1
		name := fmt.Sprintf("%s - Player %d", bp.run.ForceLabel(), num)
		p := mekbay.BattlePlayer{Number: num, Name: name, Units: bp.units[i], Crew: bp.crews[i]}
		if bp.commander != nil && bp.commander.player == i {
			p.Commander = bp.commander.unit // the record sheet marks the commander's mech
		}
		if req.Provided {
			p.Draft = playerDraft(name, bp.h, bp.units[i], bp.crews[i], bp.views, p.Commander)
		}
		req.Players = append(req.Players, p)
	}
	got, err := s.mekbay.StartBattle(ctx, req)
	if err != nil {
		log.Printf("start battle %s: %v", bp.sc.ID, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if err := s.store.SaveMekBayOperation(ctx, bp.pt.ID, bp.c.Slug, bp.sc.ID, storage.MekBayOperation{OpForInstance: got.OpForInstance, OperationID: got.OperationID, AlliesInstance: got.AlliesInstance}); err != nil {
		log.Printf("record operation: %v", err)
		http.Error(w, "The battle was started in MekBay but could not be recorded here.", http.StatusInternalServerError)
		return
	}
	s.logHistory(r, bp.pt.ID, bp.c.Slug, storage.HistoryMission, 0, battleHistory(bp))
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
	Alignment string           `json:"alignment"`        // always "friendly": the page shows the players' forces, not the OpFor's
	Allies    bool             `json:"allies,omitempty"` // the scenario's allies (its automata), not a player's force
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
		out.Forces = append(out.Forces, statusForce{Name: f.Force.Name, Alignment: f.Alignment, Allies: op.AlliesInstance != "" && f.InstanceID == op.AlliesInstance, Units: hangarViews(f.Force)})
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
	pt, c, _, ok := s.openRunRequest(w, r) // not runRequest: the battle in progress is the very thing this ends
	if !ok {
		return
	}
	sc, found := c.Scenario(r.PostFormValue("scenario"))
	if !found || len(sc.Forces) == 0 {
		http.Error(w, "unknown scenario, or it has no OpFor to deploy against", http.StatusNotFound)
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
	line := "Aborted " + c.MissionName(*sc) + ", keeping every change since the last checkpoint."
	if cps, err := s.store.Checkpoints(r.Context(), pt.ID, c.Slug); err == nil && len(cps) > 0 && mode == "rollback" {
		label, err := s.rollbackTo(r.Context(), currentSeed(r), pt.ID, c.Slug, cps[0].Seq)
		if err != nil {
			log.Printf("roll back on abort: %v", err)
			http.Error(w, "Could not roll the campaign back: "+err.Error(), http.StatusBadGateway)
			return
		}
		// The history goes back with the campaign (the deployment's line too); one line, once the battle is forgotten, says it was aborted and rolled back.
		line = "Aborted " + c.MissionName(*sc) + " and rolled back to \u201c" + label + "\u201d."
	}
	if _, err := s.store.DeleteMekBayOperation(r.Context(), pt.ID, c.Slug, sc.ID); err != nil {
		http.Error(w, "could not abort the battle", http.StatusInternalServerError)
		return
	}
	s.logHistory(r, pt.ID, c.Slug, storage.HistoryMission, 0, line) // only once the battle is gone, so the history never says it ended while it still locks the campaign
	w.WriteHeader(http.StatusNoContent)
}

// deleteBattle cleans up an aborted battle in MekBay (its operation and every force in it), in the background of the request: a failure only leaves them behind.
func (s *Server) deleteBattle(ctx context.Context, seed, operationID string) {
	if err := s.mekbay.DeleteBattle(context.WithoutCancel(ctx), mekbay.AccountUID(seed, mekbay.SlotHangar), operationID); err != nil {
		log.Printf("delete the aborted battle in MekBay: %v", err)
	}
}
