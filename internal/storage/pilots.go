package storage

import (
	"context"
	"database/sql"
	"errors"

	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/pilot"
)

// The named pilots' queries: their SP, ranks, portraits, Edge abilities and the Advanced Management corrections (see pilot.go for the rules).

const pilotColumns = `id, name, portrait, rank, fallen, sp_gunnery, sp_piloting, sp_edge_tokens, sp_edge_abilities, sp_total`

func scanPilot(row interface{ Scan(...any) error }) (p pilot.NamedPilot, err error) {
	var fallen int
	err = row.Scan(&p.ID, &p.Name, &p.Portrait, &p.Rank, &fallen, &p.SP.Gunnery, &p.SP.Piloting, &p.SP.EdgeTokens, &p.SP.EdgeAbilities, &p.TotalSP)
	p.Fallen = fallen != 0
	return p, err
}

// Pilots lists a campaign run's named pilots, oldest first, fallen ones included.
func (s *Store) Pilots(ctx context.Context, playthroughID, campaign string) ([]pilot.NamedPilot, error) {
	rows, err := s.db.QueryContext(ctx,
		s.q(`SELECT `+pilotColumns+` FROM named_pilots WHERE playthrough_id = ? AND campaign = ? ORDER BY created_at, id`),
		playthroughID, campaign)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pilot.NamedPilot
	for rows.Next() {
		p, err := scanPilot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close() // done with this result before the next query
	// The Edge abilities each pilot has chosen, in the order they chose them.
	arows, err := s.db.QueryContext(ctx,
		s.q(`SELECT a.pilot_id, a.ability FROM pilot_abilities a JOIN named_pilots p ON p.id = a.pilot_id
		     WHERE p.playthrough_id = ? AND p.campaign = ? ORDER BY a.pilot_id, a.seq`), playthroughID, campaign)
	if err != nil {
		return nil, err
	}
	defer arows.Close()
	byPilot := map[string][]string{}
	for arows.Next() {
		var pid, ability string
		if err := arows.Scan(&pid, &ability); err != nil {
			return nil, err
		}
		byPilot[pid] = append(byPilot[pid], ability)
	}
	for i := range out {
		out[i].Learned = byPilot[out[i].ID]
	}
	return out, arows.Err()
}

// lockRun makes a transaction wait for every other one that starts the same way for the same campaign run, so a check it makes (how many pilots are alive, who
// has a name or a portrait, how many abilities a pilot holds) still holds when it writes. PostgreSQL would otherwise let two such transactions both pass the
// check: its default isolation shows each only what was committed, not what the other is about to write. The statement changes nothing; it takes the run's row
// lock, held until the transaction ends. SQLite has one connection, so its transactions already run one at a time. A transaction that writes both the pilots' rows
// and the run's own takes this lock first, so that two of them never wait on each other.
func (s *Store) lockRun(ctx context.Context, tx *sql.Tx, playthroughID, campaign string) error {
	_, err := tx.ExecContext(ctx, s.q(`UPDATE campaign_runs SET warchest = warchest WHERE playthrough_id = ? AND campaign = ?`), playthroughID, campaign)
	return err
}

// AddPilot adds a pilot with the lowest rank and every SP allocated (see AddPilotWithRank).
func (s *Store) AddPilot(ctx context.Context, playthroughID, campaign, name, portrait string, start pilot.SPAllocation, limit int) (pilot.NamedPilot, error) {
	return s.AddPilotWithRank(ctx, playthroughID, campaign, name, portrait, "", start, start.Total(), limit)
}

// AddPilotWithRank adds a living named pilot to a campaign run with the given rank ("" is the lowest) and starting SP, refusing
// it when the command already has max living pilots (ErrPilotLimit), another living pilot has the portrait (ErrPortraitTaken) or
// the name is taken (ErrNameTaken, ignoring case).
func (s *Store) AddPilotWithRank(ctx context.Context, playthroughID, campaign, name, portrait, rank string, start pilot.SPAllocation, total, limit int) (pilot.NamedPilot, error) {
	total = max(total, start.Total()) // the total can't be below what is allocated; any more is unallocated
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return pilot.NamedPilot{}, err
	}
	defer tx.Rollback()
	if err := s.lockRun(ctx, tx, playthroughID, campaign); err != nil {
		return pilot.NamedPilot{}, err
	}
	var alive int
	if err := tx.QueryRowContext(ctx,
		s.q(`SELECT COUNT(*) FROM named_pilots WHERE playthrough_id = ? AND campaign = ? AND fallen = 0`),
		playthroughID, campaign).Scan(&alive); err != nil {
		return pilot.NamedPilot{}, err
	}
	if alive >= limit {
		return pilot.NamedPilot{}, pilot.ErrPilotLimit
	}
	if portrait != "" {
		var same int
		if err := tx.QueryRowContext(ctx,
			s.q(`SELECT COUNT(*) FROM named_pilots WHERE playthrough_id = ? AND campaign = ? AND fallen = 0 AND portrait = ?`),
			playthroughID, campaign, portrait).Scan(&same); err != nil {
			return pilot.NamedPilot{}, err
		}
		if same > 0 {
			return pilot.NamedPilot{}, pilot.ErrPortraitTaken
		}
	}
	var named int
	if err := tx.QueryRowContext(ctx,
		s.q(`SELECT COUNT(*) FROM named_pilots WHERE playthrough_id = ? AND campaign = ? AND LOWER(name) = LOWER(?)`),
		playthroughID, campaign, name).Scan(&named); err != nil {
		return pilot.NamedPilot{}, err
	}
	if named > 0 {
		return pilot.NamedPilot{}, ErrNameTaken
	}
	p := pilot.NamedPilot{ID: mekbay.NewID(), Name: name, Portrait: portrait, Rank: rank, SP: start, TotalSP: total}
	if _, err := tx.ExecContext(ctx,
		s.q(`INSERT INTO named_pilots (id, playthrough_id, campaign, name, portrait, rank, sp_gunnery, sp_piloting, sp_edge_tokens, sp_edge_abilities, sp_total) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		p.ID, playthroughID, campaign, name, portrait, rank, start.Gunnery, start.Piloting, start.EdgeTokens, start.EdgeAbilities, total); err != nil {
		return pilot.NamedPilot{}, err
	}
	return p, tx.Commit()
}

// AwardSP adds SP to a living pilot's columns (sql.ErrNoRows if there is no such pilot, or they have fallen).
func (s *Store) AwardSP(ctx context.Context, playthroughID, campaign, pilotID string, a pilot.SPAllocation) error {
	res, err := s.db.ExecContext(ctx,
		s.q(`UPDATE named_pilots SET sp_gunnery = sp_gunnery + ?, sp_piloting = sp_piloting + ?, sp_edge_tokens = sp_edge_tokens + ?, sp_edge_abilities = sp_edge_abilities + ?, sp_total = sp_total + ?
		     WHERE id = ? AND playthrough_id = ? AND campaign = ? AND fallen = 0`),
		a.Gunnery, a.Piloting, a.EdgeTokens, a.EdgeAbilities, a.Total(), pilotID, playthroughID, campaign)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ErrNotEnoughSP is returned when more SP is allocated than the pilot has unallocated.
var ErrNotEnoughSP = errors.New("that is more SP than the pilot has unallocated")

// EarnSP adds earned SP to a living pilot's total, unallocated until they spend it (sql.ErrNoRows if there is no such
// living pilot).
func (s *Store) EarnSP(ctx context.Context, playthroughID, campaign, pilotID string, n int) error {
	res, err := s.db.ExecContext(ctx,
		s.q(`UPDATE named_pilots SET sp_total = sp_total + ? WHERE id = ? AND playthrough_id = ? AND campaign = ? AND fallen = 0`),
		n, pilotID, playthroughID, campaign)
	if err != nil {
		return err
	}
	if r, _ := res.RowsAffected(); r == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// AllocateSP spends a living pilot's unallocated SP: a is added to their four columns, and may not exceed what is
// unallocated (total minus the columns' sum), checked in the same statement that writes it so two requests can't both
// spend the same SP. sql.ErrNoRows if there is no such living pilot, ErrNotEnoughSP if a is too much.
func (s *Store) AllocateSP(ctx context.Context, playthroughID, campaign, pilotID string, a pilot.SPAllocation) error {
	return s.ApplySP(ctx, playthroughID, campaign, pilotID, 0, a)
}

// ApplySP adds `earned` SP to a living pilot's total and allocates `a` to their columns, both in one statement, so the
// Manage SP dialog can stage earned SP and an allocation and commit them together or not at all. The allocation may use
// the pilot's unallocated SP including the SP being earned. sql.ErrNoRows if there is no such living pilot, ErrNotEnoughSP
// if a is more than that.
func (s *Store) ApplySP(ctx context.Context, playthroughID, campaign, pilotID string, earned int, a pilot.SPAllocation) error {
	res, err := s.db.ExecContext(ctx,
		s.q(`UPDATE named_pilots SET sp_total = sp_total + ?,
		       sp_gunnery = sp_gunnery + ?, sp_piloting = sp_piloting + ?, sp_edge_tokens = sp_edge_tokens + ?, sp_edge_abilities = sp_edge_abilities + ?
		     WHERE id = ? AND playthrough_id = ? AND campaign = ? AND fallen = 0
		       AND sp_total + ? - (sp_gunnery + sp_piloting + sp_edge_tokens + sp_edge_abilities) >= ?`),
		earned, a.Gunnery, a.Piloting, a.EdgeTokens, a.EdgeAbilities, pilotID, playthroughID, campaign, earned, a.Total())
	if err != nil {
		return err
	}
	if r, _ := res.RowsAffected(); r > 0 {
		return nil
	}
	var one int
	err = s.db.QueryRowContext(ctx,
		s.q(`SELECT 1 FROM named_pilots WHERE id = ? AND playthrough_id = ? AND campaign = ? AND fallen = 0`), pilotID, playthroughID, campaign).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.ErrNoRows
	}
	if err != nil {
		return err
	}
	return ErrNotEnoughSP
}

// MarkFallen records that a pilot died, which frees their place among the command's named pilots
// (sql.ErrNoRows if there is no such living pilot).
func (s *Store) MarkFallen(ctx context.Context, playthroughID, campaign, pilotID string) error {
	res, err := s.db.ExecContext(ctx,
		s.q(`UPDATE named_pilots SET fallen = 1 WHERE id = ? AND playthrough_id = ? AND campaign = ? AND fallen = 0`),
		pilotID, playthroughID, campaign)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetPilotPortrait changes a living pilot's portrait. sql.ErrNoRows if there is no such living pilot, ErrPortraitTaken if
// another living pilot of the campaign already has it.
func (s *Store) SetPilotPortrait(ctx context.Context, playthroughID, campaign, pilotID, portrait string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.lockRun(ctx, tx, playthroughID, campaign); err != nil {
		return err
	}
	var same int
	if err := tx.QueryRowContext(ctx,
		s.q(`SELECT COUNT(*) FROM named_pilots WHERE playthrough_id = ? AND campaign = ? AND fallen = 0 AND portrait = ? AND id <> ?`),
		playthroughID, campaign, portrait, pilotID).Scan(&same); err != nil {
		return err
	}
	if same > 0 {
		return pilot.ErrPortraitTaken
	}
	res, err := tx.ExecContext(ctx,
		s.q(`UPDATE named_pilots SET portrait = ? WHERE id = ? AND playthrough_id = ? AND campaign = ? AND fallen = 0`),
		portrait, pilotID, playthroughID, campaign)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

// RevivePilot brings a pilot marked killed in action back, to correct a misclick (sql.ErrNoRows if there is no such
// killed pilot, ErrPilotLimit if the command already has the most living pilots it may have). If another living pilot has
// taken the portrait in the meantime, the pilot gets altPortrait instead.
func (s *Store) RevivePilot(ctx context.Context, playthroughID, campaign, pilotID, altPortrait string, limit int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.lockRun(ctx, tx, playthroughID, campaign); err != nil {
		return err
	}
	var portrait string
	if err := tx.QueryRowContext(ctx,
		s.q(`SELECT portrait FROM named_pilots WHERE id = ? AND playthrough_id = ? AND campaign = ? AND fallen = 1`),
		pilotID, playthroughID, campaign).Scan(&portrait); err != nil {
		return err // sql.ErrNoRows when there is no such killed pilot
	}
	var alive int
	if err := tx.QueryRowContext(ctx,
		s.q(`SELECT COUNT(*) FROM named_pilots WHERE playthrough_id = ? AND campaign = ? AND fallen = 0`),
		playthroughID, campaign).Scan(&alive); err != nil {
		return err
	}
	if alive >= limit {
		return pilot.ErrPilotLimit
	}
	if portrait != "" {
		var same int
		if err := tx.QueryRowContext(ctx,
			s.q(`SELECT COUNT(*) FROM named_pilots WHERE playthrough_id = ? AND campaign = ? AND fallen = 0 AND portrait = ?`),
			playthroughID, campaign, portrait).Scan(&same); err != nil {
			return err
		}
		if same > 0 {
			portrait = altPortrait
		}
	}
	if _, err := tx.ExecContext(ctx,
		s.q(`UPDATE named_pilots SET fallen = 0, portrait = ? WHERE id = ? AND playthrough_id = ? AND campaign = ?`),
		portrait, pilotID, playthroughID, campaign); err != nil {
		return err
	}
	return tx.Commit()
}

// DeletePilot permanently removes a pilot, living or killed, to correct a misclick or a desync (sql.ErrNoRows if there is
// no such pilot).
func (s *Store) DeletePilot(ctx context.Context, playthroughID, campaign, pilotID string) error {
	if _, err := s.db.ExecContext(ctx,
		s.q(`DELETE FROM pilot_abilities WHERE pilot_id IN (SELECT id FROM named_pilots WHERE id = ? AND playthrough_id = ? AND campaign = ?)`),
		pilotID, playthroughID, campaign); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		s.q(`DELETE FROM named_pilots WHERE id = ? AND playthrough_id = ? AND campaign = ?`), pilotID, playthroughID, campaign)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ErrTotalBelowAllocated is returned when a pilot's total SP would be set below what is already allocated without
// agreeing to reset the allocation.
var ErrTotalBelowAllocated = errors.New("that total is below the SP already allocated")

// ResetPilotSP is the factory reset for a pilot's SP allocation, to repair a desync or a mistake: all four columns go back to
// zero, so the pilot is a starting 4/5 pilot again, and every SP they have earned becomes unallocated (the total is
// unchanged). The Edge abilities they had learned depended on that SP, so they are forgotten too, in the same transaction.
// It works on living and killed pilots (sql.ErrNoRows if there is no such pilot).
func (s *Store) ResetPilotSP(ctx context.Context, playthroughID, campaign, pilotID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		s.q(`UPDATE named_pilots SET sp_gunnery = 0, sp_piloting = 0, sp_edge_tokens = 0, sp_edge_abilities = 0
		     WHERE id = ? AND playthrough_id = ? AND campaign = ?`), pilotID, playthroughID, campaign)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM pilot_abilities WHERE pilot_id = ?`), pilotID); err != nil {
		return err
	}
	return tx.Commit()
}

// SetPilotTotal overrides a pilot's total SP earned. A total below what is already allocated can't be honoured without
// undoing allocations, so it is refused (ErrTotalBelowAllocated) unless resetAllocation is true, which also zeroes the
// four columns, leaving the whole new total unallocated, and forgets the pilot's learned Edge abilities (they depended on that
// SP; see ResetPilotSP). A total at or above the allocation changes neither. sql.ErrNoRows if there is no such pilot.
func (s *Store) SetPilotTotal(ctx context.Context, playthroughID, campaign, pilotID string, total int, resetAllocation bool) error {
	if resetAllocation {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		res, err := tx.ExecContext(ctx,
			s.q(`UPDATE named_pilots SET sp_total = ?, sp_gunnery = 0, sp_piloting = 0, sp_edge_tokens = 0, sp_edge_abilities = 0
			     WHERE id = ? AND playthrough_id = ? AND campaign = ?`), total, pilotID, playthroughID, campaign)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return sql.ErrNoRows
		}
		if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM pilot_abilities WHERE pilot_id = ?`), pilotID); err != nil {
			return err
		}
		return tx.Commit()
	}
	res, err := s.db.ExecContext(ctx,
		s.q(`UPDATE named_pilots SET sp_total = ?
		     WHERE id = ? AND playthrough_id = ? AND campaign = ?
		       AND ? >= sp_gunnery + sp_piloting + sp_edge_tokens + sp_edge_abilities`), total, pilotID, playthroughID, campaign, total)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	var one int
	err = s.db.QueryRowContext(ctx,
		s.q(`SELECT 1 FROM named_pilots WHERE id = ? AND playthrough_id = ? AND campaign = ?`), pilotID, playthroughID, campaign).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.ErrNoRows
	}
	if err != nil {
		return err
	}
	return ErrTotalBelowAllocated
}

// ErrNameTaken is returned when another pilot of the campaign already has the name (compared ignoring case).
var ErrNameTaken = errors.New("another pilot already has that name")

// RenamePilot changes a pilot's name, living or killed in action. Names stay unique within a campaign, ignoring case
// (ErrNameTaken); sql.ErrNoRows if there is no such pilot.
func (s *Store) RenamePilot(ctx context.Context, playthroughID, campaign, pilotID, name string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.lockRun(ctx, tx, playthroughID, campaign); err != nil {
		return err
	}
	var same int
	if err := tx.QueryRowContext(ctx,
		s.q(`SELECT COUNT(*) FROM named_pilots WHERE playthrough_id = ? AND campaign = ? AND LOWER(name) = LOWER(?) AND id <> ?`),
		playthroughID, campaign, name, pilotID).Scan(&same); err != nil {
		return err
	}
	if same > 0 {
		return ErrNameTaken
	}
	res, err := tx.ExecContext(ctx,
		s.q(`UPDATE named_pilots SET name = ? WHERE id = ? AND playthrough_id = ? AND campaign = ?`), name, pilotID, playthroughID, campaign)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

// SetPilotRank sets a pilot's cosmetic rank. The caller checks it against the campaign's ranks (sql.ErrNoRows if there is no
// such pilot).
func (s *Store) SetPilotRank(ctx context.Context, playthroughID, campaign, pilotID, rank string) error {
	res, err := s.db.ExecContext(ctx,
		s.q(`UPDATE named_pilots SET rank = ? WHERE id = ? AND playthrough_id = ? AND campaign = ?`), rank, pilotID, playthroughID, campaign)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Edge ability errors.
var (
	ErrNoPendingAbilities = errors.New("this pilot has no new Edge ability to choose")
	ErrAbilityCount       = errors.New("choose exactly as many Edge abilities as the pilot has earned")
	ErrUnknownAbility     = errors.New("that is not an Edge ability")
	ErrAbilityLearned     = errors.New("that Edge ability is already learned (or chosen twice)")
)

// LearnAbilities records the Edge abilities a living pilot chooses for the levels they have earned. The pilot may hold as many as their
// Edge abilities level; ids must name exactly as many new ones as are still pending, each from the book's list and none already learned,
// chosen together in one go. The choice is permanent until Store.ResetEdgeAbilities. sql.ErrNoRows if there is no such living pilot.
func (s *Store) LearnAbilities(ctx context.Context, playthroughID, campaign, pilotID string, ids []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.lockRun(ctx, tx, playthroughID, campaign); err != nil {
		return err
	}
	var sp int
	if err := tx.QueryRowContext(ctx,
		s.q(`SELECT sp_edge_abilities FROM named_pilots WHERE id = ? AND playthrough_id = ? AND campaign = ? AND fallen = 0`),
		pilotID, playthroughID, campaign).Scan(&sp); err != nil {
		return err // sql.ErrNoRows when there is no such living pilot
	}
	var learned int
	if err := tx.QueryRowContext(ctx, s.q(`SELECT COUNT(*) FROM pilot_abilities WHERE pilot_id = ?`), pilotID).Scan(&learned); err != nil {
		return err
	}
	pending := pilot.EdgeAbilitiesLadder.At(sp).Level - learned
	if pending <= 0 {
		return ErrNoPendingAbilities
	}
	if len(ids) != pending {
		return ErrAbilityCount
	}
	have := map[string]bool{}
	rows, err := tx.QueryContext(ctx, s.q(`SELECT ability FROM pilot_abilities WHERE pilot_id = ?`), pilotID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			rows.Close()
			return err
		}
		have[a] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if _, ok := pilot.AbilityByID(id); !ok {
			return ErrUnknownAbility
		}
		if have[id] {
			return ErrAbilityLearned
		}
		have[id] = true
	}
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO pilot_abilities (pilot_id, ability, seq) VALUES (?, ?, ?)`), pilotID, id, learned+i+1); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ResetEdgeAbilities forgets every Edge ability a pilot has learned, so they can choose them again (their SP is untouched). It works on
// living and killed pilots, to repair a mistake (sql.ErrNoRows if there is no such pilot).
func (s *Store) ResetEdgeAbilities(ctx context.Context, playthroughID, campaign, pilotID string) error {
	var one int
	if err := s.db.QueryRowContext(ctx,
		s.q(`SELECT 1 FROM named_pilots WHERE id = ? AND playthrough_id = ? AND campaign = ?`), pilotID, playthroughID, campaign).Scan(&one); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM pilot_abilities WHERE pilot_id = ?`), pilotID)
	return err
}
