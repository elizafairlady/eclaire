package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"charm.land/fantasy"
)

// Reminder is a single reminder entry.
type Reminder struct {
	ID         string    `json:"id"`
	Text       string    `json:"text"`
	DueAt      time.Time `json:"due_at"`
	Recurrence string    `json:"recurrence,omitempty"` // "daily", "weekly", or ""
	Completed  bool      `json:"completed"`
	CreatedAt  time.Time `json:"created_at"`
}

// ReminderStore manages reminders on disk.
type ReminderStore struct {
	path string
}

// NewReminderStore creates a store backed by a JSON file.
func NewReminderStore(path string) *ReminderStore {
	return &ReminderStore{path: path}
}

// Load reads all reminders from disk.
func (s *ReminderStore) Load() ([]Reminder, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var reminders []Reminder
	if err := json.Unmarshal(data, &reminders); err != nil {
		return nil, err
	}
	return reminders, nil
}

// Save writes all reminders to disk.
func (s *ReminderStore) Save(reminders []Reminder) error {
	data, err := json.MarshalIndent(reminders, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}

// Pending returns non-completed reminders sorted by due date.
func (s *ReminderStore) Pending() ([]Reminder, error) {
	all, err := s.Load()
	if err != nil {
		return nil, err
	}
	var pending []Reminder
	for _, r := range all {
		if !r.Completed {
			pending = append(pending, r)
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].DueAt.Before(pending[j].DueAt)
	})
	return pending, nil
}

// FireOverdue finds overdue reminders, marks them done (or advances recurrence),
// persists changes, and returns the fired reminders.
func (s *ReminderStore) FireOverdue() ([]Reminder, error) {
	all, err := s.Load()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var fired []Reminder
	changed := false
	for i := range all {
		r := &all[i]
		if r.Completed || !r.DueAt.Before(now) {
			continue
		}
		fired = append(fired, *r)
		if r.Recurrence != "" {
			r.DueAt = advanceRecurrence(r.DueAt, r.Recurrence)
		} else {
			r.Completed = true
		}
		changed = true
	}
	if changed {
		if err := s.Save(all); err != nil {
			return fired, err
		}
	}
	return fired, nil
}

// Overdue returns non-completed reminders past their due date.
func (s *ReminderStore) Overdue() ([]Reminder, error) {
	pending, err := s.Pending()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var overdue []Reminder
	for _, r := range pending {
		if r.DueAt.Before(now) {
			overdue = append(overdue, r)
		}
	}
	return overdue, nil
}

type reminderInput struct {
	Operation string `json:"operation" jsonschema:"description=Operation: add list done snooze"`
	Text      string `json:"text,omitempty" jsonschema:"description=Reminder text (for add)"`
	Due       string `json:"due,omitempty" jsonschema:"description=When due: duration (2h 30m 1d) or datetime (2026-04-09 09:00)"`
	ID        string `json:"id,omitempty" jsonschema:"description=Reminder ID (for done/snooze)"`
	Recurrence string `json:"recurrence,omitempty" jsonschema:"description=Recurrence: daily weekly or empty"`
	Duration  string `json:"duration,omitempty" jsonschema:"description=Snooze duration (e.g. 1h 30m)"`
}

// ReminderTool creates the eclaire_reminder tool.
func ReminderTool(store *ReminderStore) Tool {
	return NewTool("eclaire_reminder",
		"Manage reminders. Operations: add (text + due), list, done (id), snooze (id + duration).",
		TrustModify, "reminder",
		func(ctx context.Context, input reminderInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			switch input.Operation {
			case "add":
				return handleReminderAdd(store, input)
			case "list":
				return handleReminderList(store)
			case "done":
				return handleReminderDone(store, input)
			case "snooze":
				return handleReminderSnooze(store, input)
			default:
				return fantasy.NewTextErrorResponse(
					fmt.Sprintf("unknown operation %q; valid: add, list, done, snooze", input.Operation),
				), nil
			}
		},
	)
}

func handleReminderAdd(store *ReminderStore, input reminderInput) (fantasy.ToolResponse, error) {
	if input.Text == "" {
		return fantasy.NewTextErrorResponse("text is required"), nil
	}
	if input.Due == "" {
		return fantasy.NewTextErrorResponse("due is required (e.g. '2h', '30m', '1d', '2026-04-09 09:00')"), nil
	}

	dueAt, err := parseDue(input.Due)
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("invalid due: %v", err)), nil
	}

	all, _ := store.Load()

	reminder := Reminder{
		ID:         fmt.Sprintf("r%d", time.Now().UnixNano()%1_000_000_000),
		Text:       input.Text,
		DueAt:      dueAt,
		Recurrence: input.Recurrence,
		CreatedAt:  time.Now(),
	}
	all = append(all, reminder)

	if err := store.Save(all); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("save: %v", err)), nil
	}

	return fantasy.ToolResponse{
		Content: fmt.Sprintf("Reminder added: %s (due %s, id=%s)", reminder.Text, reminder.DueAt.Format("2006-01-02 15:04"), reminder.ID),
	}, nil
}

func handleReminderList(store *ReminderStore) (fantasy.ToolResponse, error) {
	pending, err := store.Pending()
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("load: %v", err)), nil
	}

	if len(pending) == 0 {
		return fantasy.ToolResponse{Content: "No pending reminders."}, nil
	}

	now := time.Now()
	var sb strings.Builder
	for _, r := range pending {
		status := "upcoming"
		if r.DueAt.Before(now) {
			status = "OVERDUE"
		}
		recur := ""
		if r.Recurrence != "" {
			recur = fmt.Sprintf(" [%s]", r.Recurrence)
		}
		sb.WriteString(fmt.Sprintf("- [%s] %s — due %s (%s)%s\n", r.ID, r.Text, r.DueAt.Format("2006-01-02 15:04"), status, recur))
	}
	return fantasy.ToolResponse{Content: sb.String()}, nil
}

func handleReminderDone(store *ReminderStore, input reminderInput) (fantasy.ToolResponse, error) {
	if input.ID == "" {
		return fantasy.NewTextErrorResponse("id is required"), nil
	}

	all, err := store.Load()
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("load: %v", err)), nil
	}

	found := false
	for i, r := range all {
		if r.ID == input.ID {
			if r.Recurrence != "" {
				// Recurring: advance due date instead of completing
				all[i].DueAt = advanceRecurrence(r.DueAt, r.Recurrence)
				if err := store.Save(all); err != nil {
					return fantasy.NewTextErrorResponse(fmt.Sprintf("save: %v", err)), nil
				}
				return fantasy.ToolResponse{
					Content: fmt.Sprintf("Recurring reminder %q advanced to %s.", r.Text, all[i].DueAt.Format("2006-01-02 15:04")),
				}, nil
			}
			all[i].Completed = true
			found = true
			break
		}
	}

	if !found {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("reminder %q not found", input.ID)), nil
	}

	if err := store.Save(all); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("save: %v", err)), nil
	}

	return fantasy.ToolResponse{Content: fmt.Sprintf("Reminder %s marked done.", input.ID)}, nil
}

func handleReminderSnooze(store *ReminderStore, input reminderInput) (fantasy.ToolResponse, error) {
	if input.ID == "" {
		return fantasy.NewTextErrorResponse("id is required"), nil
	}
	if input.Duration == "" {
		return fantasy.NewTextErrorResponse("duration is required (e.g. '1h', '30m')"), nil
	}

	dur, err := parseDuration(input.Duration)
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("invalid duration: %v", err)), nil
	}

	all, err := store.Load()
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("load: %v", err)), nil
	}

	found := false
	var newDue time.Time
	for i, r := range all {
		if r.ID == input.ID {
			all[i].DueAt = time.Now().Add(dur)
			newDue = all[i].DueAt
			found = true
			break
		}
	}

	if !found {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("reminder %q not found", input.ID)), nil
	}

	if err := store.Save(all); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("save: %v", err)), nil
	}

	return fantasy.ToolResponse{
		Content: fmt.Sprintf("Reminder %s snoozed until %s.", input.ID, newDue.Format("2006-01-02 15:04")),
	}, nil
}

// parseDue parses a due time from a duration, natural language, or absolute datetime.
// Supported formats:
//   - Duration: "2h", "30m", "1d", "2h30m"
//   - Natural: "in 2 hours", "in 30 minutes", "today", "tomorrow", "tonight"
//   - Datetime: "2006-01-02 15:04", "2006-01-02T15:04:05", "2006-01-02", "15:04"
func parseDue(s string) (time.Time, error) {
	lower := strings.ToLower(strings.TrimSpace(s))
	now := time.Now()

	// Natural language shortcuts
	switch lower {
	case "today":
		return time.Date(now.Year(), now.Month(), now.Day(), 17, 0, 0, 0, now.Location()), nil
	case "tonight":
		return time.Date(now.Year(), now.Month(), now.Day(), 21, 0, 0, 0, now.Location()), nil
	case "tomorrow":
		t := now.Add(24 * time.Hour)
		return time.Date(t.Year(), t.Month(), t.Day(), 9, 0, 0, 0, now.Location()), nil
	case "tomorrow morning":
		t := now.Add(24 * time.Hour)
		return time.Date(t.Year(), t.Month(), t.Day(), 8, 0, 0, 0, now.Location()), nil
	case "tomorrow evening":
		t := now.Add(24 * time.Hour)
		return time.Date(t.Year(), t.Month(), t.Day(), 18, 0, 0, 0, now.Location()), nil
	}

	// "in N hours", "in N minutes", "in Nh", "in Nm"
	if strings.HasPrefix(lower, "in ") {
		rest := strings.TrimPrefix(lower, "in ")
		// Try parsing "N hours", "N minutes", "N days"
		var n int
		var unit string
		if _, err := fmt.Sscanf(rest, "%d %s", &n, &unit); err == nil {
			switch {
			case strings.HasPrefix(unit, "hour"):
				return now.Add(time.Duration(n) * time.Hour), nil
			case strings.HasPrefix(unit, "min"):
				return now.Add(time.Duration(n) * time.Minute), nil
			case strings.HasPrefix(unit, "day"):
				return now.Add(time.Duration(n) * 24 * time.Hour), nil
			}
		}
		// Try as raw duration ("in 2h", "in 30m")
		if dur, err := parseDuration(rest); err == nil {
			return now.Add(dur), nil
		}
	}

	// Try duration first ("2h", "30m", "1d")
	if dur, err := parseDuration(s); err == nil {
		return now.Add(dur), nil
	}

	// Try datetime formats
	for _, layout := range []string{
		"2006-01-02 15:04",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
		"15:04",
		"3:04pm",
		"3pm",
	} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			// For date-only, set to 9am
			if layout == "2006-01-02" {
				t = time.Date(t.Year(), t.Month(), t.Day(), 9, 0, 0, 0, time.Local)
			}
			// For time-only, assume today (or tomorrow if past)
			if layout == "15:04" || layout == "3:04pm" || layout == "3pm" {
				t = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, time.Local)
				if t.Before(now) {
					t = t.Add(24 * time.Hour)
				}
			}
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("cannot parse %q as duration or datetime", s)
}

// parseDuration parses "2h", "30m", "1d", "2h30m" etc.
func parseDuration(s string) (time.Duration, error) {
	// Handle "d" suffix for days
	if strings.HasSuffix(s, "d") {
		s = strings.TrimSuffix(s, "d")
		var days int
		if _, err := fmt.Sscanf(s, "%d", &days); err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func advanceRecurrence(current time.Time, recurrence string) time.Time {
	switch recurrence {
	case "daily":
		return current.Add(24 * time.Hour)
	case "weekly":
		return current.Add(7 * 24 * time.Hour)
	default:
		return current.Add(24 * time.Hour)
	}
}
