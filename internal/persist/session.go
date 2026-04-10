package persist

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SessionEvent types.
const (
	EventUserMessage      = "user_message"
	EventAssistantMessage = "assistant_message"
	EventAssistantDelta   = "assistant_delta"
	EventToolCall         = "tool_call"
	EventToolResult       = "tool_result"
	EventSystemMessage    = "system_message"
	EventChildSpawned     = "child_spawned"
	EventChildCompleted   = "child_completed"
	EventCompaction       = "compaction"
	EventMemoryFlush      = "memory_flush"
	EventStepFinish       = "step_finish"
	EventTodoUpdate       = "todo_update"
)

// SessionMeta is lightweight metadata stored in meta.json.
type SessionMeta struct {
	ID           string    `json:"id"`
	ParentID     string    `json:"parent_id,omitempty"`
	RootID       string    `json:"root_id,omitempty"`
	AgentID      string    `json:"agent_id"`
	Title        string    `json:"title"`
	Status       string    `json:"status"` // active, completed, archived, error
	Kind         string    `json:"kind,omitempty"`         // "", "main", "project"
	ProjectRoot  string    `json:"project_root,omitempty"` // for kind=project
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int       `json:"message_count"`
	TokensIn     int64     `json:"tokens_in"`
	TokensOut    int64     `json:"tokens_out"`
	ChildIDs     []string  `json:"child_ids,omitempty"`

	// Session-scoped approval patterns (command patterns approved by user).
	// Keyed by "agentID:toolName" → list of glob patterns.
	ApprovalPatterns map[string][]string `json:"approval_patterns,omitempty"`
}

// SessionEvent is a single entry in the append-only JSONL event log.
type SessionEvent struct {
	Seq       int64           `json:"seq"`
	Timestamp time.Time       `json:"ts"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
}

// MessageData is the Data payload for message events.
type MessageData struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content"`
	AgentID string `json:"agent_id,omitempty"`
}

// ToolCallData is the Data payload for tool call events.
type ToolCallData struct {
	Name       string `json:"name"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Input      string `json:"input,omitempty"`
	Output     string `json:"output,omitempty"`
	AgentID    string `json:"agent_id,omitempty"`
}

// ChildData is the Data payload for child_spawned/child_completed events.
type ChildData struct {
	ChildID      string `json:"child_id"`
	ChildAgentID string `json:"child_agent_id"`
	Title        string `json:"title,omitempty"`
	Result       string `json:"result,omitempty"`
}

// StepData is the Data payload for step_finish events.
type StepData struct {
	TokensIn  int64 `json:"tokens_in"`
	TokensOut int64 `json:"tokens_out"`
}

// TodoData is the Data payload for todo_update events.
type TodoData struct {
	Todos []TodoItem `json:"todos"`
}

// TodoItem is a single todo entry persisted in session events.
type TodoItem struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"active_form"`
}

// SessionLock prevents concurrent writes to the same session.
type SessionLock struct {
	locks map[string]*sync.Mutex
	mu    sync.Mutex
}

func newSessionLock() *SessionLock {
	return &SessionLock{locks: make(map[string]*sync.Mutex)}
}

func (l *SessionLock) For(sessionID string) *sync.Mutex {
	l.mu.Lock()
	defer l.mu.Unlock()
	m, ok := l.locks[sessionID]
	if !ok {
		m = &sync.Mutex{}
		l.locks[sessionID] = m
	}
	return m
}

// SessionStore manages sessions as directories with JSONL event logs.
type SessionStore struct {
	dir  string
	lock *SessionLock
}

// NewSessionStore creates a session store.
func NewSessionStore(dir string) *SessionStore {
	return &SessionStore{dir: dir, lock: newSessionLock()}
}

// Create makes a new session directory with meta.json and empty events.jsonl.
func (s *SessionStore) Create(agentID, title, parentID string) (*SessionMeta, error) {
	id := uuid.NewString()[:8]
	rootID := id
	if parentID != "" {
		// Inherit root from parent
		parent, err := s.GetMeta(parentID)
		if err == nil && parent.RootID != "" {
			rootID = parent.RootID
		} else {
			rootID = parentID
		}
	}

	meta := &SessionMeta{
		ID:        id,
		ParentID:  parentID,
		RootID:    rootID,
		AgentID:   agentID,
		Title:     title,
		Status:    "active",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	sessDir := s.sessDir(id)
	if err := os.MkdirAll(sessDir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir session %s: %w", id, err)
	}

	if err := s.saveMeta(meta); err != nil {
		return nil, err
	}

	// Create empty events file
	evPath := filepath.Join(sessDir, "events.jsonl")
	f, err := os.Create(evPath)
	if err != nil {
		return nil, fmt.Errorf("create events %s: %w", id, err)
	}
	f.Close()

	// Link to parent
	if parentID != "" {
		s.addChild(parentID, id)
	}

	return meta, nil
}

// SpawnChild creates a child session linked to a parent.
func (s *SessionStore) SpawnChild(parentID, childAgentID, title string) (*SessionMeta, error) {
	return s.Create(childAgentID, title, parentID)
}

// MainSessionID is the well-known deterministic ID for the global main session.
const MainSessionID = "main"

// GetOrCreateMain returns the persistent main session, creating it if needed.
// Idempotent — always returns the same session.
func (s *SessionStore) GetOrCreateMain(agentID string) (*SessionMeta, error) {
	// Try to load existing
	meta, err := s.GetMeta(MainSessionID)
	if err == nil {
		return meta, nil
	}

	// Create with deterministic ID
	meta = &SessionMeta{
		ID:        MainSessionID,
		AgentID:   agentID,
		Title:     "Main Session",
		Status:    "active",
		Kind:      "main",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	sessDir := s.sessDir(MainSessionID)
	if err := os.MkdirAll(sessDir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir main session: %w", err)
	}
	if err := s.saveMeta(meta); err != nil {
		return nil, err
	}
	evPath := filepath.Join(sessDir, "events.jsonl")
	f, err := os.Create(evPath)
	if err != nil {
		return nil, fmt.Errorf("create main events: %w", err)
	}
	f.Close()

	return meta, nil
}

// FindByProject finds an active project session for the given project root.
// Returns nil if none found.
func (s *SessionStore) FindByProject(projectRoot string) *SessionMeta {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		meta, err := s.GetMeta(e.Name())
		if err != nil {
			continue
		}
		if meta.Kind == "project" && meta.ProjectRoot == projectRoot && meta.Status == "active" {
			return meta
		}
	}
	return nil
}

// CreateProject creates or resumes a project-scoped session.
func (s *SessionStore) CreateProject(agentID, projectRoot string) (*SessionMeta, error) {
	// Check for existing
	if existing := s.FindByProject(projectRoot); existing != nil {
		return existing, nil
	}

	// Derive title from project directory name
	title := filepath.Base(projectRoot)

	id := uuid.NewString()[:8]
	meta := &SessionMeta{
		ID:          id,
		AgentID:     agentID,
		Title:       title,
		Status:      "active",
		Kind:        "project",
		ProjectRoot: projectRoot,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	sessDir := s.sessDir(id)
	if err := os.MkdirAll(sessDir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir project session: %w", err)
	}
	if err := s.saveMeta(meta); err != nil {
		return nil, err
	}
	evPath := filepath.Join(sessDir, "events.jsonl")
	f, err := os.Create(evPath)
	if err != nil {
		return nil, fmt.Errorf("create project events: %w", err)
	}
	f.Close()

	return meta, nil
}

// SavePatterns persists approval patterns to session metadata.
func (s *SessionStore) SavePatterns(sessionID string, patterns map[string][]string) error {
	meta, err := s.GetMeta(sessionID)
	if err != nil {
		return err
	}
	meta.ApprovalPatterns = patterns
	return s.saveMeta(meta)
}

// GetMeta loads session metadata.
func (s *SessionStore) GetMeta(id string) (*SessionMeta, error) {
	path := filepath.Join(s.sessDir(id), "meta.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read meta %s: %w", id, err)
	}
	var meta SessionMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse meta %s: %w", id, err)
	}
	return &meta, nil
}

// Append writes an event to the session's JSONL log.
func (s *SessionStore) Append(sessionID string, evType string, data any) error {
	mu := s.lock.For(sessionID)
	mu.Lock()
	defer mu.Unlock()

	rawData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal event data: %w", err)
	}

	ev := SessionEvent{
		Seq:       time.Now().UnixNano(),
		Timestamp: time.Now(),
		Type:      evType,
		Data:      rawData,
	}

	line, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	evPath := filepath.Join(s.sessDir(sessionID), "events.jsonl")
	f, err := os.OpenFile(evPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("open events %s: %w", sessionID, err)
	}
	defer f.Close()

	_, err = f.Write(append(line, '\n'))
	if err != nil {
		return fmt.Errorf("write event %s: %w", sessionID, err)
	}

	// Update meta
	meta, merr := s.GetMeta(sessionID)
	if merr == nil {
		meta.MessageCount++
		meta.UpdatedAt = time.Now()
		if evType == EventStepFinish {
			var step StepData
			json.Unmarshal(rawData, &step)
			meta.TokensIn += step.TokensIn
			meta.TokensOut += step.TokensOut
		}
		s.saveMeta(meta)
	}

	return nil
}

// ReadEvents reads all events from a session's JSONL log.
func (s *SessionStore) ReadEvents(sessionID string) ([]SessionEvent, error) {
	evPath := filepath.Join(s.sessDir(sessionID), "events.jsonl")
	f, err := os.Open(evPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open events %s: %w", sessionID, err)
	}
	defer f.Close()

	var events []SessionEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB line limit
	for scanner.Scan() {
		var ev SessionEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue // skip malformed lines
		}
		events = append(events, ev)
	}
	return events, scanner.Err()
}

// List returns metadata for all sessions, most recent first.
func (s *SessionStore) List() ([]SessionMeta, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	var metas []SessionMeta
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "archive" {
			continue
		}
		meta, err := s.GetMeta(entry.Name())
		if err != nil {
			continue
		}
		metas = append(metas, *meta)
	}

	sort.Slice(metas, func(i, j int) bool {
		return metas[i].UpdatedAt.After(metas[j].UpdatedAt)
	})

	return metas, nil
}

// UpdateStatus sets a session's status.
func (s *SessionStore) UpdateStatus(id, status string) error {
	meta, err := s.GetMeta(id)
	if err != nil {
		return err
	}
	meta.Status = status
	meta.UpdatedAt = time.Now()
	return s.saveMeta(meta)
}

// UpdateTitle sets a session's title.
func (s *SessionStore) UpdateTitle(id, title string) error {
	meta, err := s.GetMeta(id)
	if err != nil {
		return err
	}
	meta.Title = title
	meta.UpdatedAt = time.Now()
	return s.saveMeta(meta)
}

// Delete removes a session directory.
func (s *SessionStore) Delete(id string) error {
	return os.RemoveAll(s.sessDir(id))
}

// Archive moves a session to the archive directory.
func (s *SessionStore) Archive(id string) error {
	archiveDir := filepath.Join(s.dir, "archive", time.Now().Format("2006-01-02"))
	if err := os.MkdirAll(archiveDir, 0o700); err != nil {
		return err
	}
	return os.Rename(s.sessDir(id), filepath.Join(archiveDir, id))
}

// internal helpers

func (s *SessionStore) sessDir(id string) string {
	return filepath.Join(s.dir, id)
}

func (s *SessionStore) saveMeta(meta *SessionMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	return os.WriteFile(filepath.Join(s.sessDir(meta.ID), "meta.json"), data, 0o644)
}

func (s *SessionStore) addChild(parentID, childID string) {
	mu := s.lock.For(parentID)
	mu.Lock()
	defer mu.Unlock()

	meta, err := s.GetMeta(parentID)
	if err != nil {
		return
	}
	meta.ChildIDs = append(meta.ChildIDs, childID)
	meta.UpdatedAt = time.Now()
	s.saveMeta(meta)
}
