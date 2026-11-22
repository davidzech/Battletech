package site

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/storage"
)

// Checkpoints: the campaign's rollback points.
//
// Everything the player does between missions (repairs, sales, purchases, pilots' SP) changes the live campaign in place, as ever. A checkpoint is taken when the
// campaign starts and right after each mission is completed: a snapshot of the campaign's rows in the database, and a frozen copy of the hangar force in MekBay (a
// force of its own that nothing touches). Rolling back to one restores the rows and copies the frozen hangar back over the live one, so the campaign is exactly as
// it was at that moment. Aborting a mission rolls back to the latest checkpoint, which is the end of the previous mission. A campaign keeps at most
// maxCheckpoints; the oldest are dropped and their frozen hangars deleted, as are the frozen hangars of every checkpoint after one rolled back to.

// ---- MekBay: frozen copies

// ---- the server

// checkpointCopy makes the frozen hangar copy a new checkpoint will hold: a copy of the run's live hangar as it is now. It returns "" for a run whose hangar the
// site doesn't own (nothing to freeze).
func (s *Server) checkpointCopy(ctx context.Context, seed string, run storage.CampaignRun) (string, error) {
	if !run.Owned() {
		return "", nil
	}
	id := mekbay.NewID()
	if err := s.mekbay.CopyForce(ctx, mekbay.CopyRequest{OwnerUID: mekbay.AccountUID(seed, mekbay.SlotHangar), Source: run.HangarInstance, Target: id}); err != nil {
		return "", err
	}
	return id, nil
}

// deleteHangars deletes forces the site made in MekBay for a seed, in the background of the request (a failure only leaves unused forces behind).
func (s *Server) deleteHangars(ctx context.Context, seed string, instances []string) {
	if len(instances) == 0 {
		return
	}
	if err := s.mekbay.DeleteForce(context.WithoutCancel(ctx), mekbay.AccountUID(seed, mekbay.SlotHangar), instances...); err != nil {
		log.Printf("delete %d unused forces in MekBay: %v", len(instances), err)
	}
}

// recordCheckpoint snapshots the campaign as it is now as a new checkpoint holding the frozen hangar copy; the frozen hangars of checkpoints dropped for age are
// deleted. If it can't be recorded the copy is deleted again.
func (s *Server) recordCheckpoint(ctx context.Context, seed, playthroughID, campaign, label, copyID string) {
	dropped, err := s.store.AddCheckpoint(ctx, playthroughID, campaign, label, copyID)
	if err != nil {
		log.Printf("record a checkpoint (%s): %v", label, err)
		if copyID != "" {
			s.deleteHangars(ctx, seed, []string{copyID})
		}
		return
	}
	s.deleteHangars(ctx, seed, dropped)
}

// rollbackTo puts the campaign back as it was at a checkpoint: the frozen hangar is copied over the live one first (if that fails nothing has changed), then the
// database rows are restored and the later checkpoints forgotten, and their frozen hangars deleted.
func (s *Server) rollbackTo(ctx context.Context, seed, playthroughID, campaign string, seq int) error {
	cp, ok, err := s.store.Checkpoint(ctx, playthroughID, campaign, seq)
	if err != nil {
		return err
	}
	if !ok {
		return sql.ErrNoRows
	}
	cur, _, err := s.store.Run(ctx, playthroughID, campaign)
	if err != nil {
		return err
	}
	live := storage.SnapshotHangarInstance(cp.Snapshot)
	if cp.HangarCopy != "" && live != "" {
		if err := s.mekbay.CopyForce(ctx, mekbay.CopyRequest{OwnerUID: mekbay.AccountUID(seed, mekbay.SlotHangar), Source: cp.HangarCopy, Target: live}); err != nil {
			return fmt.Errorf("could not restore the hangar in MekBay: %w", err)
		}
	}
	dropped, err := s.store.RestoreCheckpoint(ctx, playthroughID, campaign, seq)
	if err != nil {
		return err
	}
	if cur.Owned() && cur.HangarInstance != live && cur.HangarInstance != "" {
		dropped = append(dropped, cur.HangarInstance) // a hangar imported since is not part of the campaign any more
	}
	s.deleteHangars(ctx, seed, dropped)
	return nil
}

// rollbackCampaign is POST /api/campaign/rollback: restore the campaign to one of its checkpoints.
func (s *Server) rollbackCampaign(w http.ResponseWriter, r *http.Request) {
	pt, c, ok := s.pilotRequest(w, r)
	if !ok {
		return
	}
	seq, err := strconv.Atoi(r.PostFormValue("checkpoint"))
	if err != nil {
		http.Error(w, "Choose a checkpoint.", http.StatusBadRequest)
		return
	}
	switch err := s.rollbackTo(r.Context(), currentSeed(r), pt.ID, c.Slug, seq); {
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "That checkpoint no longer exists.", http.StatusNotFound)
	case err != nil:
		log.Printf("roll back: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// CheckpointView is a checkpoint as the Manage page lists it.
type CheckpointView struct {
	Seq    int
	Label  string
	When   string
	Latest bool
}

func checkpointViews(cps []storage.Checkpoint) []CheckpointView {
	out := make([]CheckpointView, 0, len(cps))
	for i, c := range cps {
		out = append(out, CheckpointView{Seq: c.Seq, Label: c.Label, When: time.Unix(c.At, 0).UTC().Format("2006-01-02 15:04 UTC"), Latest: i == 0})
	}
	return out
}
