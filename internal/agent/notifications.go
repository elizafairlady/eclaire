package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/elizafairlady/eclaire/internal/bus"
	"github.com/google/uuid"
)

// NotifySeverity indicates notification importance.
type NotifySeverity string

const (
	SeverityDebug   NotifySeverity = "debug"
	SeverityInfo    NotifySeverity = "info"
	SeverityWarning NotifySeverity = "warning"
	SeverityError   NotifySeverity = "error"
)

// Notification is a persistent message for the user.
type Notification struct {
	ID        string         `json:"id"`
	Severity  NotifySeverity `json:"severity"`
	Source    string         `json:"source"` // "heartbeat", "cron", "agent", "system", "reminder", "approval"
	Title     string         `json:"title"`
	Content   string         `json:"content"`
	AgentID   string         `json:"agent_id,omitempty"`
	JobID     string         `json:"job_id,omitempty"`
	RefID     string         `json:"ref_id,omitempty"` // source-specific reference (reminder ID, approval request ID)
	Actions   []string       `json:"actions,omitempty"` // available actions for this notification
	CreatedAt time.Time      `json:"created_at"`
	Read      bool           `json:"read"`
	Resolved  bool           `json:"resolved"` // true after an action has been taken
}

// ActionsForSource returns the available actions for a notification source.
func ActionsForSource(source string) []string {
	switch source {
	case "reminder":
		return []string{"complete", "dismiss", "snooze"}
	case "approval":
		return []string{"yes", "always", "no"}
	default:
		return []string{"dismiss"}
	}
}

// NotificationFilter controls what notifications are returned.
type NotificationFilter struct {
	Severity   NotifySeverity // empty = all
	Source     string         // empty = all
	UnreadOnly bool
	Limit      int // 0 = no limit
}

// NotificationStore persists notifications as JSONL and caches recent ones in memory.
type NotificationStore struct {
	path    string
	entries []Notification
	mu      sync.Mutex
}

// NewNotificationStore creates or loads a notification store.
func NewNotificationStore(path string) (*NotificationStore, error) {
	s := &NotificationStore{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// SubscribeToBus connects the notification store to bus events.
// Creates notifications from BackgroundResult events.
func (s *NotificationStore) SubscribeToBus(ctx context.Context, b *bus.Bus) {
	b.SubscribeFunc(ctx, bus.TopicBackgroundResult, func(ev bus.Event) {
		br, ok := ev.Payload.(bus.BackgroundResult)
		if !ok {
			return
		}
		sev := SeverityInfo
		if br.Status == "error" {
			sev = SeverityWarning
		}
		n := Notification{
			Severity: sev,
			Source:   br.Source,
			Title:    fmt.Sprintf("%s: %s", br.Source, br.TaskName),
			Content:  br.Content,
			AgentID:  br.AgentID,
			RefID:    br.RefID,
			Actions:  ActionsForSource(br.Source),
		}
		s.Add(n)
	})
}

// Add appends a notification. ID and CreatedAt are set automatically if empty.
func (s *NotificationStore) Add(n Notification) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if n.ID == "" {
		n.ID = uuid.NewString()[:8]
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}

	s.entries = append(s.entries, n)

	// Keep in-memory cache bounded
	if len(s.entries) > 1000 {
		s.entries = s.entries[len(s.entries)-1000:]
	}

	return s.appendToFile(n)
}

// Pending returns all unread notifications (newest first).
func (s *NotificationStore) Pending() []Notification {
	return s.List(NotificationFilter{UnreadOnly: true})
}

// Drain returns all pending notifications and marks them as read.
func (s *NotificationStore) Drain() []Notification {
	s.mu.Lock()
	defer s.mu.Unlock()

	var pending []Notification
	for i := len(s.entries) - 1; i >= 0; i-- {
		if !s.entries[i].Read {
			pending = append(pending, s.entries[i])
			s.entries[i].Read = true
		}
	}
	if len(pending) > 0 {
		s.rewrite()
	}
	return pending
}

// List returns notifications matching the filter (newest first).
func (s *NotificationStore) List(filter NotificationFilter) []Notification {
	s.mu.Lock()
	defer s.mu.Unlock()

	var result []Notification
	for i := len(s.entries) - 1; i >= 0; i-- {
		n := s.entries[i]
		if filter.UnreadOnly && (n.Read || n.Resolved) {
			continue
		}
		if filter.Severity != "" && n.Severity != filter.Severity {
			continue
		}
		if filter.Source != "" && n.Source != filter.Source {
			continue
		}
		result = append(result, n)
		if filter.Limit > 0 && len(result) >= filter.Limit {
			break
		}
	}
	return result
}

// Get returns a notification by ID.
func (s *NotificationStore) Get(id string) *Notification {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.entries {
		if s.entries[i].ID == id {
			n := s.entries[i]
			return &n
		}
	}
	return nil
}

// Resolve marks a notification as resolved (action taken).
func (s *NotificationStore) Resolve(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.entries {
		if s.entries[i].ID == id {
			s.entries[i].Resolved = true
			s.entries[i].Read = true
			s.rewrite()
			return true
		}
	}
	return false
}

// MarkRead marks a single notification as read.
func (s *NotificationStore) MarkRead(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.entries {
		if s.entries[i].ID == id {
			s.entries[i].Read = true
			s.rewrite()
			return true
		}
	}
	return false
}

// MarkAllRead marks all notifications as read.
func (s *NotificationStore) MarkAllRead() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for i := range s.entries {
		if !s.entries[i].Read {
			s.entries[i].Read = true
			count++
		}
	}
	if count > 0 {
		s.rewrite()
	}
	return count
}

// Count returns total and unread counts.
func (s *NotificationStore) Count() (total, unread int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range s.entries {
		total++
		if !n.Read {
			unread++
		}
	}
	return
}

func (s *NotificationStore) load() error {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 256*1024)
	for scanner.Scan() {
		var n Notification
		if json.Unmarshal(scanner.Bytes(), &n) == nil {
			s.entries = append(s.entries, n)
		}
	}

	// Keep bounded on load
	if len(s.entries) > 1000 {
		s.entries = s.entries[len(s.entries)-1000:]
	}
	return scanner.Err()
}

func (s *NotificationStore) appendToFile(n Notification) error {
	dir := filepath.Dir(s.path)
	if dir != "" && dir != "." {
		os.MkdirAll(dir, 0o700)
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	data, _ := json.Marshal(n)
	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}

func (s *NotificationStore) rewrite() error {
	dir := filepath.Dir(s.path)
	if dir != "" && dir != "." {
		os.MkdirAll(dir, 0o700)
	}
	tmp := s.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	for _, n := range s.entries {
		data, _ := json.Marshal(n)
		f.Write(data)
		f.Write([]byte{'\n'})
	}
	f.Close()
	return os.Rename(tmp, s.path)
}
