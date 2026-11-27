package site

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/davidzech/Battletech/internal/storage"
)

// Checkpoints: the campaign's rollback points.
//
// Everything the player does between missions (repairs, sales, purchases, pilots' SP) changes the live campaign in place, as ever. A checkpoint is taken when the
// campaign starts and right after each mission is completed: a snapshot of the campaign's rows in the database, the hangar among them (it is part of the run's
// row). Rolling back to one restores the rows in one transaction, so the campaign is exactly as it was at that moment. Aborting a mission rolls back to the latest
// checkpoint, which is the end of the previous mission. A campaign keeps at most storage.MaxCheckpoints; the oldest are dropped.

// cleanupTimeout bounds a background clean-up in MekBay.
const cleanupTimeout = 2 * time.Minute

// cleanUpInBackground deletes battles (operations with every force in them) that nothing on the site refers to any more, in a goroutine of its own: the player
// doesn't wait for MekBay, and a failure only leaves them behind there, which affects nothing (it is logged).
func (s *Server) cleanUpInBackground(seed string, operations []string) {
	if len(operations) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		for _, op := range operations {
			s.deleteBattle(ctx, seed, op)
		}
	}()
}

// recordCheckpoint snapshots the campaign as it is now as a new checkpoint. A failure only costs the rollback point, so it is logged.
func (s *Server) recordCheckpoint(ctx context.Context, playthroughID, campaign, label string) {
	if err := s.store.AddCheckpoint(context.WithoutCancel(ctx), playthroughID, campaign, label); err != nil {
		log.Printf("record a checkpoint (%s): %v", label, err)
	}
}

// rollbackTo puts the campaign back as it was at a checkpoint, its hangar too, and forgets the later checkpoints, all in one transaction. It returns the
// checkpoint's label.
func (s *Server) rollbackTo(ctx context.Context, playthroughID, campaign string, seq int) (string, error) {
	cp, ok, err := s.store.Checkpoint(ctx, playthroughID, campaign, seq)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", sql.ErrNoRows
	}
	if err := s.store.RestoreCheckpoint(ctx, playthroughID, campaign, seq); err != nil {
		return "", err
	}
	return cp.Label, nil
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
	label, err := s.rollbackTo(r.Context(), pt.ID, c.Slug, seq)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "That checkpoint no longer exists.", http.StatusNotFound)
	case err != nil:
		log.Printf("roll back: %v", err)
		http.Error(w, "Could not roll the campaign back.", http.StatusInternalServerError)
	default:
		// The lines after the checkpoint are gone with the rollback; this one says it happened.
		s.logHistory(r, pt.ID, c.Slug, storage.HistoryCorrections, 0, "Rolled back to “"+label+"”.")
		w.WriteHeader(http.StatusNoContent)
	}
}
