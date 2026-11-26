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
// storage.MaxCheckpoints; the oldest are dropped and their frozen hangars deleted, as are the frozen hangars of every checkpoint after one rolled back to.

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

// cleanupTimeout bounds a background clean-up in MekBay.
const cleanupTimeout = 2 * time.Minute

// cleanUpInBackground deletes forces and battles (operations with every force in them) that nothing on the site refers to any more, in a goroutine of its own:
// the player doesn't wait for MekBay, and a failure only leaves them behind there, which affects nothing (it is logged).
func (s *Server) cleanUpInBackground(seed string, forces, operations []string) {
	if len(forces) == 0 && len(operations) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		for _, op := range operations {
			s.deleteBattle(ctx, seed, op)
		}
		s.deleteHangars(ctx, seed, forces)
	}()
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

// errRollbackHangar marks a rollback that failed in MekBay (rather than in the database), whose message can be shown to the player.
var errRollbackHangar = errors.New("could not restore the hangar in MekBay")

// rollbackFailure is what a failed rollback answers: MekBay's own explanation when the hangar could not be restored, and a fixed message for anything else (a
// database error is logged, never sent).
func rollbackFailure(err error) (string, int) {
	if errors.Is(err, errRollbackHangar) {
		return err.Error(), http.StatusBadGateway
	}
	return "Could not roll the campaign back.", http.StatusInternalServerError
}

// rollbackTo puts the campaign back as it was at a checkpoint: the frozen hangar is copied over the live one, then the database rows are restored and the later
// checkpoints forgotten, and their frozen hangars deleted. The two halves fail together: the live hangar is first set aside in a copy of its own, and if the rows
// can't be restored it is put back, so a failed rollback leaves the campaign as it was (hangar and rows alike) and can simply be tried again. If a copy fails
// before the rows are touched nothing has changed.
func (s *Server) rollbackTo(ctx context.Context, seed, playthroughID, campaign string, seq int) (string, error) {
	cp, ok, err := s.store.Checkpoint(ctx, playthroughID, campaign, seq)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", sql.ErrNoRows
	}
	cur, _, err := s.store.Run(ctx, playthroughID, campaign)
	if err != nil {
		return "", err
	}
	owner := mekbay.AccountUID(seed, mekbay.SlotHangar)
	live := storage.SnapshotHangarInstance(cp.Snapshot)
	aside := "" // the copy the live hangar is set aside in while the rows are restored
	if cp.HangarCopy != "" && live != "" {
		aside = mekbay.NewID()
		if err := s.mekbay.CopyForce(ctx, mekbay.CopyRequest{OwnerUID: owner, Source: live, Target: aside}); err != nil {
			s.deleteHangars(ctx, seed, []string{aside}) // whatever part of it was saved
			return "", fmt.Errorf("%w: %w", errRollbackHangar, err)
		}
		if err := s.mekbay.CopyForce(ctx, mekbay.CopyRequest{OwnerUID: owner, Source: cp.HangarCopy, Target: live}); err != nil {
			s.deleteHangars(ctx, seed, []string{aside})
			return "", fmt.Errorf("%w: %w", errRollbackHangar, err)
		}
	}
	dropped, err := s.store.RestoreCheckpoint(ctx, playthroughID, campaign, seq)
	if err != nil {
		if aside != "" {
			// The rows are as they were, so the hangar goes back to how it was too. This is not abandoned if the browser goes away.
			if back := s.mekbay.CopyForce(context.WithoutCancel(ctx), mekbay.CopyRequest{OwnerUID: owner, Source: aside, Target: live}); back != nil {
				// The hangar is the checkpoint's and the rows are not. The copy set aside is kept: it is the only record of the hangar as it was.
				log.Printf("roll back: the rows could not be restored (%v) and the hangar could not be put back, it is kept in force %s: %v", err, aside, back)
				return "", err
			}
			s.deleteHangars(ctx, seed, []string{aside})
		}
		return "", err
	}
	if aside != "" {
		dropped = append(dropped, aside)
	}
	if cur.Owned() && cur.HangarInstance != live && cur.HangarInstance != "" {
		dropped = append(dropped, cur.HangarInstance) // a hangar imported since is not part of the campaign any more
	}
	s.deleteHangars(ctx, seed, dropped)
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
	label, err := s.rollbackTo(r.Context(), currentSeed(r), pt.ID, c.Slug, seq)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "That checkpoint no longer exists.", http.StatusNotFound)
	case err != nil:
		log.Printf("roll back: %v", err)
		msg, code := rollbackFailure(err)
		http.Error(w, msg, code)
	default:
		// The lines after the checkpoint are gone with the rollback; this one says it happened.
		s.logHistory(r, pt.ID, c.Slug, storage.HistoryCorrections, 0, "Rolled back to \u201c"+label+"\u201d.")
		w.WriteHeader(http.StatusNoContent)
	}
}
