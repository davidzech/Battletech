package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"
)

// HangarClaimTTL is how long a claim to edit a run's hangar holds without being released: longer than the slowest MekBay edit (a battle's write-back has 45
// seconds), short enough that a server that died mid-edit doesn't lock the hangar for long.
const HangarClaimTTL = 2 * time.Minute

// ClaimHangarEdit claims the right to edit a campaign run's hangar in MekBay. An edit is read-modify-save, so two at once would both start from the same
// force and the later save would undo the earlier one; the claim makes them take turns even when they run on different servers sharing the database. Only
// one claim holds at a time (a single UPDATE of the run's row, as in ClaimBattle); ok is false while another claim holds that hasn't expired, or when there
// is no such run. The token returned releases the claim (ReleaseHangarEdit), so a claim that expired and was taken over is never released by its first holder.
func (s *Store) ClaimHangarEdit(ctx context.Context, playthroughID, campaign string) (token string, ok bool, err error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", false, err
	}
	token = hex.EncodeToString(b[:])
	now := time.Now()
	res, err := s.db.ExecContext(ctx, s.q(`UPDATE campaign_runs SET hangar_claim = ?, hangar_claim_token = ?
		WHERE playthrough_id = ? AND campaign = ? AND hangar_claim < ?`),
		now.Unix(), token, playthroughID, campaign, now.Add(-HangarClaimTTL).Unix())
	if err != nil {
		return "", false, err
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return "", false, err
	}
	return token, true, nil
}

// ReleaseHangarEdit ends a claim made with ClaimHangarEdit, if the claim is still the one the token names.
func (s *Store) ReleaseHangarEdit(ctx context.Context, playthroughID, campaign, token string) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE campaign_runs SET hangar_claim = 0, hangar_claim_token = '' WHERE playthrough_id = ? AND campaign = ? AND hangar_claim_token = ?`),
		playthroughID, campaign, token)
	return err
}
