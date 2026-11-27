package site

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/storage"
)

// The one-time import of the hangars MekBay used to hold. Before hangars lived in the database, each campaign run's hangar was a force in MekBay and each
// checkpoint held a frozen copy of it there. ImportHangars reads every one the database doesn't hold yet, as it is (damage, ammunition and all), and stores it:
// a run's into its row, a checkpoint's into its snapshot. Nothing is changed or deleted in MekBay. It runs when the server starts, before it serves anything.
//
// Each hangar is handled once: storing it, or skipping one that can't be imported, forgets its MekBay force in the same write, so a later start lists nothing
// and never contacts MekBay.

// importTimeout bounds the whole import.
const importTimeout = 10 * time.Minute

// ImportHangars imports the hangars still in MekBay (see above) with the MekBay client mb (nil is the real one).
//
// What trying again can't fix is logged and skipped (and marked handled): a force MekBay no longer has (mekbay.ErrNoForce), data that isn't a force
// (mekbay.ParseForce refuses it), a checkpoint whose snapshot can't take a hangar (storage.ErrBadSnapshot). A campaign left without a hangar says so on its
// pages and can only be deleted and started again; a checkpoint without one can't be rolled back to. Anything else (MekBay unreachable or slow, a database
// error) stops the import with an error, so the server doesn't start on a database with hangars missing; what was imported before stays imported, and the
// next start carries on.
func ImportHangars(ctx context.Context, store *storage.Store, mb mekbay.MekBay) error {
	if mb == nil {
		mb = &mekbay.Client{}
	}
	ctx, cancel := context.WithTimeout(ctx, importTimeout)
	defer cancel()
	if err := store.ForgetImportedHangarSources(ctx); err != nil {
		return fmt.Errorf("forget the hangars already imported: %w", err)
	}
	runs, err := store.RunHangarImports(ctx)
	if err != nil {
		return fmt.Errorf("list the hangars to import: %w", err)
	}
	checkpoints, err := store.CheckpointHangarImports(ctx)
	if err != nil {
		return fmt.Errorf("list the checkpoints' hangars to import: %w", err)
	}
	if len(runs) == 0 && len(checkpoints) == 0 {
		return nil
	}
	log.Printf("importing hangars from MekBay: %d campaigns, %d checkpoints", len(runs), len(checkpoints))
	// read reads a force from MekBay. A failure that trying again can't fix is answered as skip (why), with no error.
	read := func(h storage.HangarImport) (data []byte, skip string, err error) {
		data, err = mb.ReadForce(ctx, h.Instance)
		if errors.Is(err, mekbay.ErrNoForce) {
			return nil, "MekBay no longer has it", nil
		}
		if err != nil {
			return nil, "", err
		}
		if _, err := mekbay.ParseForce(data); err != nil {
			return nil, fmt.Sprintf("MekBay's data for it is not a force (%v)", err), nil
		}
		return data, "", nil
	}
	var imported, skipped, already int
	for _, h := range runs {
		what := fmt.Sprintf("the hangar of campaign %s (playthrough %.8s, force %s)", h.Campaign, h.PlaythroughID, h.Instance)
		data, skip, err := read(h)
		if err != nil {
			return fmt.Errorf("import %s: %w", what, err)
		}
		if skip == "" && h.SourceURL == "" {
			// A run from before the site kept its own copy: its hangar is the player's own force, whose crews may be named pilots. The hangar never holds one.
			if data, err = mekbay.ResetCrews(data); err != nil {
				skip = fmt.Sprintf("its crews could not be reset (%v)", err)
			}
		}
		if skip != "" {
			log.Printf("hangar import: skipping %s: %s; the campaign is left without a hangar", what, skip)
			if err := store.SkipHangarImport(ctx, h.PlaythroughID, h.Campaign); err != nil {
				return fmt.Errorf("mark %s as skipped: %w", what, err)
			}
			skipped++
			continue
		}
		stored, err := store.ImportHangar(ctx, h.PlaythroughID, h.Campaign, data)
		if err != nil {
			return fmt.Errorf("store %s: %w", what, err)
		}
		if stored {
			imported++
		} else {
			already++ // another server imported it first
		}
	}
	for _, h := range checkpoints {
		what := fmt.Sprintf("the hangar of checkpoint %d of campaign %s (playthrough %.8s, force %s)", h.Seq, h.Campaign, h.PlaythroughID, h.Instance)
		data, skip, err := read(h)
		if err != nil {
			return fmt.Errorf("import %s: %w", what, err)
		}
		stored := false
		if skip == "" {
			stored, err = store.ImportCheckpointHangar(ctx, h.PlaythroughID, h.Campaign, h.Seq, data)
			if errors.Is(err, storage.ErrBadSnapshot) {
				skip, err = err.Error(), nil
			}
			if err != nil {
				return fmt.Errorf("store %s: %w", what, err)
			}
		}
		if skip != "" {
			log.Printf("hangar import: skipping %s: %s; the checkpoint can't be rolled back to", what, skip)
			if err := store.SkipCheckpointHangarImport(ctx, h.PlaythroughID, h.Campaign, h.Seq); err != nil {
				return fmt.Errorf("mark %s as skipped: %w", what, err)
			}
			skipped++
			continue
		}
		if stored {
			imported++
		} else {
			already++
		}
	}
	log.Printf("imported hangars from MekBay: %d imported, %d skipped (can't be imported), %d already imported elsewhere", imported, skipped, already)
	return nil
}
