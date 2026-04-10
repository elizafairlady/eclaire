package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const maxRunLogBytes = 2 * 1024 * 1024 // 2MB per job log

// RunLogEntry records a single job execution.
type RunLogEntry struct {
	Timestamp time.Time     `json:"timestamp"`
	JobID     string        `json:"job_id"`
	Status    string        `json:"status"` // "ok", "error", "skipped"
	Error     string        `json:"error,omitempty"`
	Summary   string        `json:"summary,omitempty"`
	Duration  time.Duration `json:"duration_ms"` // serialized as milliseconds
	AgentID   string        `json:"agent_id,omitempty"`
	SessionID string        `json:"session_id,omitempty"`
}

// RunLog stores per-job execution history as JSONL files.
type RunLog struct {
	dir string
	mu  sync.Mutex
}

// NewRunLog creates a run log backed by the given directory.
func NewRunLog(dir string) *RunLog {
	return &RunLog{dir: dir}
}

// Append writes an entry to the job's run log.
func (r *RunLog) Append(entry RunLogEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		return err
	}
	path := r.jobPath(entry.JobID)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open run log: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err := f.Write(data); err != nil {
		return err
	}

	// Check size and prune if needed
	info, err := f.Stat()
	if err == nil && info.Size() > maxRunLogBytes {
		f.Close()
		r.prune(path)
	}
	return nil
}

// Read returns the last `limit` entries for a job (newest first).
// If limit <= 0, returns all entries.
func (r *RunLog) Read(jobID string, limit int) ([]RunLogEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	path := r.jobPath(jobID)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []RunLogEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 256*1024)
	for scanner.Scan() {
		var e RunLogEntry
		if json.Unmarshal(scanner.Bytes(), &e) == nil {
			entries = append(entries, e)
		}
	}

	// Reverse for newest-first
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, scanner.Err()
}

func (r *RunLog) jobPath(jobID string) string {
	return filepath.Join(r.dir, jobID+".jsonl")
}

// prune keeps the last 2000 lines of a run log file.
func (r *RunLog) prune(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 256*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	f.Close()

	if len(lines) <= 2000 {
		return
	}
	lines = lines[len(lines)-2000:]

	tmp := path + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return
	}
	for _, line := range lines {
		out.WriteString(line + "\n")
	}
	out.Close()
	os.Rename(tmp, path)
}
