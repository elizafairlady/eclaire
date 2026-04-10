package persist

import (
	"time"
)

// ReapCompleted archives sessions that are completed or errored and older than
// the retention duration. Main and project sessions are never reaped.
// Returns the count of archived sessions.
//
// Reference: OpenClaw src/cron/session-reaper.ts — sweeps cron run sessions
// on timer tick, 24h retention, throttled to 5min intervals.
func (s *SessionStore) ReapCompleted(retention time.Duration) (int, error) {
	sessions, err := s.List()
	if err != nil {
		return 0, err
	}

	cutoff := time.Now().Add(-retention)
	archived := 0

	for _, meta := range sessions {
		// Never reap main or project sessions
		if meta.Kind == "main" || meta.Kind == "project" {
			continue
		}

		// Only reap completed or error sessions
		if meta.Status != "completed" && meta.Status != "error" {
			continue
		}

		// Only reap sessions older than retention period
		if meta.UpdatedAt.After(cutoff) {
			continue
		}

		if err := s.Archive(meta.ID); err != nil {
			continue // skip failures, try the rest
		}
		archived++
	}

	return archived, nil
}

// CleanupStale marks stale "active" sessions as "completed" and archives them.
// A session is stale if it has status "active", is not the main or project session,
// and hasn't been updated since the staleness threshold.
// This handles orphaned sessions from before session lifecycle was wired up.
func (s *SessionStore) CleanupStale(staleAfter time.Duration) (marked, archived int, err error) {
	sessions, err := s.List()
	if err != nil {
		return 0, 0, err
	}

	cutoff := time.Now().Add(-staleAfter)

	for _, meta := range sessions {
		if meta.Kind == "main" || meta.Kind == "project" {
			continue
		}
		if meta.Status != "active" {
			continue
		}
		if meta.UpdatedAt.After(cutoff) {
			continue
		}

		// Mark as completed
		if err := s.UpdateStatus(meta.ID, "completed"); err != nil {
			continue
		}
		marked++

		// Archive immediately
		if err := s.Archive(meta.ID); err != nil {
			continue
		}
		archived++
	}

	return marked, archived, nil
}
