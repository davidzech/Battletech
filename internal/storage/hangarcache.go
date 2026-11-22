package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/davidzech/Battletech/internal/mekbay"
)

// SaveHangarCache stores the hangar last read from MekBay for a campaign run, replacing the previous copy.
func (s *Store) SaveHangarCache(ctx context.Context, playthroughID, campaign, instance string, h mekbay.HangarForce) error {
	data, err := json.Marshal(h)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		s.q(`INSERT INTO hangar_cache (playthrough_id, campaign, instance, data, fetched_at) VALUES (?, ?, ?, ?, ?)
		     ON CONFLICT (playthrough_id, campaign) DO UPDATE SET instance = excluded.instance, data = excluded.data, fetched_at = excluded.fetched_at`),
		playthroughID, campaign, instance, string(data), time.Now().Unix())
	return err
}

// CachedHangar is the hangar last read for a campaign run, and when. ok is false when there is none, or when it was read from a
// different hangar link than instance (the run's link has changed since).
func (s *Store) CachedHangar(ctx context.Context, playthroughID, campaign, instance string) (h mekbay.HangarForce, at time.Time, ok bool, err error) {
	var data string
	var unix int64
	err = s.db.QueryRowContext(ctx,
		s.q(`SELECT data, fetched_at FROM hangar_cache WHERE playthrough_id = ? AND campaign = ? AND instance = ?`),
		playthroughID, campaign, instance).Scan(&data, &unix)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return mekbay.HangarForce{}, time.Time{}, false, nil
		}
		return mekbay.HangarForce{}, time.Time{}, false, err
	}
	if err := json.Unmarshal([]byte(data), &h); err != nil {
		return mekbay.HangarForce{}, time.Time{}, false, nil // an unreadable copy is no copy
	}
	return h, time.Unix(unix, 0), true, nil
}
