package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type flowStoreFile struct {
	Version int       `json:"version"`
	Runs    []FlowRun `json:"runs"`
}

// FlowStore persists flow run state to disk.
type FlowStore struct {
	path string
	runs map[string]*FlowRun
	mu   sync.RWMutex
}

// NewFlowStore loads or creates a flow store at the given path.
func NewFlowStore(path string) (*FlowStore, error) {
	s := &FlowStore{
		path: path,
		runs: make(map[string]*FlowRun),
	}
	if err := s.load(); err != nil {
		return s, err
	}
	return s, nil
}

// Save upserts a flow run and persists to disk.
func (s *FlowStore) Save(run *FlowRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *run
	s.runs[run.ID] = &cp
	return s.save()
}

// Get returns a copy of a flow run by ID.
func (s *FlowStore) Get(id string) (*FlowRun, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.runs[id]
	if !ok {
		return nil, false
	}
	cp := *r
	return &cp, true
}

// List returns all flow runs sorted by CreatedAt descending.
func (s *FlowStore) List() []FlowRun {
	s.mu.RLock()
	defer s.mu.RUnlock()
	runs := make([]FlowRun, 0, len(s.runs))
	for _, r := range s.runs {
		runs = append(runs, *r)
	}
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].CreatedAt.After(runs[j].CreatedAt)
	})
	return runs
}

// ClearStaleRunning marks any "running" flow as failed (stale from restart).
func (s *FlowStore) ClearStaleRunning() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, r := range s.runs {
		if r.Status == FlowRunning {
			r.Status = FlowFailed
			r.Error = "stale: gateway restarted"
			count++
		}
	}
	if count > 0 {
		s.save()
	}
	return count
}

func (s *FlowStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read flow store: %w", err)
	}
	var f flowStoreFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse flow store: %w", err)
	}
	for i := range f.Runs {
		r := f.Runs[i]
		s.runs[r.ID] = &r
	}
	return nil
}

func (s *FlowStore) save() error {
	// Prune completed/failed runs older than 30 days
	cutoff := time.Now().AddDate(0, 0, -30)
	for id, r := range s.runs {
		if r.Status != FlowRunning && r.CreatedAt.Before(cutoff) {
			delete(s.runs, id)
		}
	}

	runs := make([]FlowRun, 0, len(s.runs))
	for _, r := range s.runs {
		runs = append(runs, *r)
	}
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].CreatedAt.Before(runs[j].CreatedAt)
	})

	f := flowStoreFile{Version: 1, Runs: runs}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal flow store: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write flow store tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename flow store: %w", err)
	}
	return nil
}
