package site

import (
	"context"
	"database/sql"
	"errors"
)

// A battle in progress is a row of mekbay_operations: its MekBay operation and OpFor, and the last status read of it (see battle.go).

// MekBayOperation records the MekBay force and operation the site synthesized for one scenario of a campaign run.
type MekBayOperation struct {
	OpForInstance string // the OpFor force's instance ID
	OperationID   string
}

// MekBayOperation loads the stored operation for a scenario; ok is false if none was created yet.
func (s *Store) MekBayOperation(ctx context.Context, playthroughID, campaign, scenarioID string) (op MekBayOperation, ok bool, err error) {
	err = s.db.QueryRowContext(ctx,
		s.q(`SELECT opfor_instance, operation_id FROM mekbay_operations WHERE playthrough_id = ? AND campaign = ? AND scenario_id = ?`),
		playthroughID, campaign, scenarioID,
	).Scan(&op.OpForInstance, &op.OperationID)
	if errors.Is(err, sql.ErrNoRows) {
		return MekBayOperation{}, false, nil
	}
	return op, err == nil, err
}

// DeleteMekBayOperation forgets a scenario's operation: the battle is over (the mission was completed or aborted). Nothing is deleted in MekBay.
func (s *Store) DeleteMekBayOperation(ctx context.Context, playthroughID, campaign, scenarioID string) (bool, error) {
	res, err := s.db.ExecContext(ctx, s.q(`DELETE FROM mekbay_operations WHERE playthrough_id = ? AND campaign = ? AND scenario_id = ?`), playthroughID, campaign, scenarioID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SaveBattleStatus keeps the last status read of a battle (the players' forces, as JSON), so the Operation in progress view can show it at once on the next visit
// while a fresh read is made (see the hangar cache, which does the same for the hangar). It does nothing if the battle is gone.
func (s *Store) SaveBattleStatus(ctx context.Context, playthroughID, campaign, scenarioID, forcesJSON string, at int64) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE mekbay_operations SET status_json = ?, status_at = ? WHERE playthrough_id = ? AND campaign = ? AND scenario_id = ?`), forcesJSON, at, playthroughID, campaign, scenarioID)
	return err
}

// BattleStatusCache returns what SaveBattleStatus kept: the forces as JSON and when they were read (zero and empty if never).
func (s *Store) BattleStatusCache(ctx context.Context, playthroughID, campaign, scenarioID string) (forcesJSON string, at int64, err error) {
	err = s.db.QueryRowContext(ctx, s.q(`SELECT status_json, status_at FROM mekbay_operations WHERE playthrough_id = ? AND campaign = ? AND scenario_id = ?`), playthroughID, campaign, scenarioID).Scan(&forcesJSON, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, nil
	}
	return forcesJSON, at, err
}

// SaveMekBayOperation stores (or replaces) the operation for a scenario of a campaign run.
func (s *Store) SaveMekBayOperation(ctx context.Context, playthroughID, campaign, scenarioID string, op MekBayOperation) error {
	_, err := s.db.ExecContext(ctx,
		s.q(`INSERT INTO mekbay_operations (playthrough_id, campaign, scenario_id, opfor_instance, operation_id) VALUES (?, ?, ?, ?, ?)
		     ON CONFLICT (playthrough_id, campaign, scenario_id) DO UPDATE SET opfor_instance = excluded.opfor_instance, operation_id = excluded.operation_id, status_json = '', status_at = 0`),
		playthroughID, campaign, scenarioID, op.OpForInstance, op.OperationID)
	return err
}
