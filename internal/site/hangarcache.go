package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"time"
)

// A pull-through cache of each campaign run's MekBay hangar. Reading a hangar from MekBay takes a moment, and a page that has
// to wait for it shows an empty "reading your hangar" state. So every successful live read is also saved in the database
// (table hangar_cache, one row per playthrough and campaign), and the pages that show the hangar embed the last saved copy:
// they paint it straight away, then read MekBay again in the background and replace it with the fresh data. A run that has
// never been read shows the loading state, once. The copy belongs to the hangar link it was read from: change the link and the
// old copy is ignored.

// forceData is the hangar as the campaign overview shows it: every unit, and the base BV they sum to (an unknown unit adds
// nothing; a destroyed one still counts, being still in the hangar).
func forceData(h HangarForce) map[string]any {
	units := hangarViews(h)
	bv := 0
	for _, u := range units {
		if !u.Unknown {
			bv += u.BV
		}
	}
	return map[string]any{"name": h.Name, "units": units, "bv": bv}
}

// deployData is the hangar as the scenario's deploy popup shows it.
func deployData(h HangarForce, limit int, canModify, hasOperation bool) map[string]any {
	return map[string]any{"name": h.Name, "limit": limit, "canModify": canModify, "units": hangarViews(h), "hasOperation": hasOperation}
}

func cachedJSON(data map[string]any, at time.Time) string {
	data["cachedAt"] = at.UTC().Format(time.RFC3339)
	b, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	return string(b)
}

// ForceJSON is the cached hangar for the campaign overview, as the JSON the overview script renders ("" if there is none yet).
func (p Page) ForceJSON() string {
	if p.CachedHangar == nil {
		return ""
	}
	return cachedJSON(forceData(*p.CachedHangar), p.CachedAt)
}

// DeployJSON is the cached hangar for the scenario's deploy popup ("" if there is none yet).
func (p Page) DeployJSON() string {
	if p.CachedHangar == nil || p.Scenario == nil {
		return ""
	}
	return cachedJSON(deployData(*p.CachedHangar, p.Scenario.BVLimit, p.Play.CanModify(), p.MekBayOp != ""), p.CachedAt)
}

// cacheHangar saves a hangar that was just read live. A failure only costs the cache, so it is logged and otherwise ignored.
func (s *Server) cacheHangar(ctx context.Context, playthroughID, campaign, instance string, h HangarForce) {
	if err := s.store.SaveHangarCache(ctx, playthroughID, campaign, instance, h); err != nil {
		log.Printf("cache hangar: %v", err)
	}
}

// SaveHangarCache stores the hangar last read from MekBay for a campaign run, replacing the previous copy.
func (s *Store) SaveHangarCache(ctx context.Context, playthroughID, campaign, instance string, h HangarForce) error {
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
func (s *Store) CachedHangar(ctx context.Context, playthroughID, campaign, instance string) (h HangarForce, at time.Time, ok bool, err error) {
	var data string
	var unix int64
	err = s.db.QueryRowContext(ctx,
		s.q(`SELECT data, fetched_at FROM hangar_cache WHERE playthrough_id = ? AND campaign = ? AND instance = ?`),
		playthroughID, campaign, instance).Scan(&data, &unix)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return HangarForce{}, time.Time{}, false, nil
		}
		return HangarForce{}, time.Time{}, false, err
	}
	if err := json.Unmarshal([]byte(data), &h); err != nil {
		return HangarForce{}, time.Time{}, false, nil // an unreadable copy is no copy
	}
	return h, time.Unix(unix, 0), true, nil
}
