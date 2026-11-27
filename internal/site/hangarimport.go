package site

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/storage"
)

// The one-time import of the hangars MekBay used to hold. Before hangars lived in the database, each campaign run's hangar was a force in MekBay and each
// checkpoint held a frozen copy of it there. ImportHangars reads every one the database doesn't hold yet, as it is (damage, ammunition and all), and stores it:
// a run's into its row, a checkpoint's into its snapshot. Nothing is changed or deleted in MekBay. It runs when the server starts, before it serves anything,
// and does nothing once everything is imported.

// importTimeout bounds the whole import.
const importTimeout = 10 * time.Minute

// ImportHangars imports the hangars still in MekBay (see above) with the MekBay client mb (nil is the real one). It stops at the first failure and returns it,
// so the server doesn't start on a database with hangars missing; what was imported before stays imported, and the next start carries on.
func ImportHangars(ctx context.Context, store *storage.Store, mb mekbay.MekBay) error {
	if mb == nil {
		mb = &mekbay.Client{}
	}
	ctx, cancel := context.WithTimeout(ctx, importTimeout)
	defer cancel()
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
	read := func(h storage.HangarImport) ([]byte, error) {
		data, err := mb.ReadForce(ctx, h.Instance)
		if err != nil {
			return nil, err
		}
		if _, err := mekbay.ParseForce(data); err != nil {
			return nil, err
		}
		return data, nil
	}
	for _, h := range runs {
		data, err := read(h)
		if err != nil {
			return fmt.Errorf("import the hangar of campaign %s (playthrough %.8s, force %s): %w", h.Campaign, h.PlaythroughID, h.Instance, err)
		}
		if err := store.SetHangar(ctx, h.PlaythroughID, h.Campaign, data); err != nil {
			return fmt.Errorf("store the hangar of campaign %s (playthrough %.8s): %w", h.Campaign, h.PlaythroughID, err)
		}
	}
	for _, h := range checkpoints {
		data, err := read(h)
		if err != nil {
			return fmt.Errorf("import the hangar of checkpoint %d of campaign %s (playthrough %.8s, force %s): %w", h.Seq, h.Campaign, h.PlaythroughID, h.Instance, err)
		}
		if err := store.SetCheckpointHangar(ctx, h.PlaythroughID, h.Campaign, h.Seq, data); err != nil {
			return fmt.Errorf("store the hangar of checkpoint %d of campaign %s (playthrough %.8s): %w", h.Seq, h.Campaign, h.PlaythroughID, err)
		}
	}
	log.Printf("imported hangars from MekBay: %d campaigns, %d checkpoints", len(runs), len(checkpoints))
	return nil
}
