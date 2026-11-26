package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// A battle in progress is a row of mekbay_operations: its MekBay operation and OpFor, and the last status read of it (see internal/mekbay/battle.go).

// MekBayOperation records the MekBay force and operation the site synthesized for one scenario of a campaign run.
type MekBayOperation struct {
	OpForInstance string // the OpFor force's instance ID
	OperationID   string
	// AlliesInstance is the scenario's allies' force ("" when it has none): friendly, but not the players', so its units never go back into the hangar.
	AlliesInstance string
}

// MekBayOperation loads the stored operation for a scenario; ok is false if none was created yet.
func (s *Store) MekBayOperation(ctx context.Context, playthroughID, campaign, scenarioID string) (op MekBayOperation, ok bool, err error) {
	err = s.db.QueryRowContext(ctx,
		s.q(`SELECT opfor_instance, operation_id, allies_instance FROM mekbay_operations WHERE playthrough_id = ? AND campaign = ? AND scenario_id = ?`),
		playthroughID, campaign, scenarioID,
	).Scan(&op.OpForInstance, &op.OperationID, &op.AlliesInstance)
	if errors.Is(err, sql.ErrNoRows) {
		return MekBayOperation{}, false, nil
	}
	return op, err == nil, err
}

// MekBayOperationIDs lists the MekBay operation of every battle a campaign run has recorded, the one in progress and any a rollback or an older version of the
// site left behind, so that deleting the campaign can delete them in MekBay too.
func (s *Store) MekBayOperationIDs(ctx context.Context, playthroughID, campaign string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT operation_id FROM mekbay_operations WHERE playthrough_id = ? AND campaign = ? AND operation_id <> ''`), playthroughID, campaign)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
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
		s.q(`INSERT INTO mekbay_operations (playthrough_id, campaign, scenario_id, opfor_instance, operation_id, allies_instance) VALUES (?, ?, ?, ?, ?, ?)
		     ON CONFLICT (playthrough_id, campaign, scenario_id) DO UPDATE SET opfor_instance = excluded.opfor_instance, operation_id = excluded.operation_id, allies_instance = excluded.allies_instance, status_json = '', status_at = 0`),
		playthroughID, campaign, scenarioID, op.OpForInstance, op.OperationID, op.AlliesInstance)
	return err
}

// BattleScenarios lists the scenarios of a campaign run that have a battle in progress, oldest scenario first, leaving out any whose scenario is already
// completed (a row an older version of the site, or a rollback to an old checkpoint, left behind). While one is in progress the campaign is locked: nothing
// but that battle's own actions may change it. The site also drops scenarios the campaign no longer has.
func (s *Store) BattleScenarios(ctx context.Context, playthroughID, campaign string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT o.scenario_id FROM mekbay_operations o
		WHERE o.playthrough_id = ? AND o.campaign = ? AND NOT EXISTS (SELECT 1 FROM scenario_completions c
			WHERE c.playthrough_id = o.playthrough_id AND c.campaign = o.campaign AND c.scenario_id = o.scenario_id)
		ORDER BY o.scenario_id`), playthroughID, campaign)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// BattleClaimTTL is how long a claim to start a battle holds without being released: long enough for the slowest build in MekBay, short enough that a
// server that died mid-start doesn't lock the campaign for long.
const BattleClaimTTL = 10 * time.Minute

// ClaimBattle claims the right to start a battle in a campaign run, for the scenario named, while its forces are built in MekBay. Only one claim holds at a
// time (a single UPDATE of the run's row, which both SQLite and Postgres apply one at a time), so two starts at once can't both go ahead; ok is false when
// another start holds a claim that hasn't expired. The claim is released with ReleaseBattleClaim once the battle is recorded (or has failed).
func (s *Store) ClaimBattle(ctx context.Context, playthroughID, campaign, scenarioID string) (ok bool, err error) {
	now := time.Now()
	res, err := s.db.ExecContext(ctx, s.q(`UPDATE campaign_runs SET battle_claim = ?, battle_claim_scenario = ?
		WHERE playthrough_id = ? AND campaign = ? AND battle_claim < ?`),
		now.Unix(), scenarioID, playthroughID, campaign, now.Add(-BattleClaimTTL).Unix())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// BattleClaim says which scenario a start in progress has claimed (see ClaimBattle), if any claim holds.
func (s *Store) BattleClaim(ctx context.Context, playthroughID, campaign string) (scenarioID string, ok bool, err error) {
	var at int64
	err = s.db.QueryRowContext(ctx, s.q(`SELECT battle_claim, battle_claim_scenario FROM campaign_runs WHERE playthrough_id = ? AND campaign = ?`),
		playthroughID, campaign).Scan(&at, &scenarioID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if at < time.Now().Add(-BattleClaimTTL).Unix() {
		return "", false, nil
	}
	return scenarioID, true, nil
}

// ReleaseBattleClaim ends a claim made with ClaimBattle.
func (s *Store) ReleaseBattleClaim(ctx context.Context, playthroughID, campaign string) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE campaign_runs SET battle_claim = 0, battle_claim_scenario = '' WHERE playthrough_id = ? AND campaign = ?`),
		playthroughID, campaign)
	return err
}
