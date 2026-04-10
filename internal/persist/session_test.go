package persist

import (
	"testing"
)

func TestSessionCreate(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionStore(dir)

	meta, err := store.Create("coder", "Test Session", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if meta.ID == "" {
		t.Error("ID should not be empty")
	}
	if meta.AgentID != "coder" {
		t.Errorf("AgentID = %q", meta.AgentID)
	}
	if meta.Title != "Test Session" {
		t.Errorf("Title = %q", meta.Title)
	}
	if meta.Status != "active" {
		t.Errorf("Status = %q", meta.Status)
	}
	if meta.ParentID != "" {
		t.Errorf("ParentID = %q, want empty", meta.ParentID)
	}
	if meta.RootID != meta.ID {
		t.Errorf("RootID = %q, want %q (self for root sessions)", meta.RootID, meta.ID)
	}
}

func TestSessionGetMeta(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionStore(dir)

	created, _ := store.Create("coder", "Get Test", "")
	got, err := store.GetMeta(created.ID)
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if got.Title != "Get Test" {
		t.Errorf("Title = %q", got.Title)
	}
}

func TestSessionAppendAndReadEvents(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionStore(dir)

	meta, _ := store.Create("coder", "Events Test", "")

	err := store.Append(meta.ID, EventUserMessage, MessageData{Content: "hello"})
	if err != nil {
		t.Fatalf("Append user: %v", err)
	}

	err = store.Append(meta.ID, EventAssistantMessage, MessageData{Content: "hi there"})
	if err != nil {
		t.Fatalf("Append assistant: %v", err)
	}

	err = store.Append(meta.ID, EventToolCall, ToolCallData{Name: "shell", Input: "ls"})
	if err != nil {
		t.Fatalf("Append tool_call: %v", err)
	}

	events, err := store.ReadEvents(meta.ID)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	if events[0].Type != EventUserMessage {
		t.Errorf("events[0].Type = %q", events[0].Type)
	}
	if events[1].Type != EventAssistantMessage {
		t.Errorf("events[1].Type = %q", events[1].Type)
	}
	if events[2].Type != EventToolCall {
		t.Errorf("events[2].Type = %q", events[2].Type)
	}
}

func TestSessionSpawnChild(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionStore(dir)

	parent, _ := store.Create("orchestrator", "Parent", "")
	child, err := store.SpawnChild(parent.ID, "coding", "Implement login")
	if err != nil {
		t.Fatalf("SpawnChild: %v", err)
	}

	if child.ParentID != parent.ID {
		t.Errorf("child.ParentID = %q, want %q", child.ParentID, parent.ID)
	}
	if child.RootID != parent.ID {
		t.Errorf("child.RootID = %q, want %q", child.RootID, parent.ID)
	}
	if child.AgentID != "coding" {
		t.Errorf("child.AgentID = %q", child.AgentID)
	}

	// Parent should have child in ChildIDs
	updatedParent, _ := store.GetMeta(parent.ID)
	if len(updatedParent.ChildIDs) != 1 {
		t.Fatalf("parent.ChildIDs len = %d", len(updatedParent.ChildIDs))
	}
	if updatedParent.ChildIDs[0] != child.ID {
		t.Errorf("parent.ChildIDs[0] = %q", updatedParent.ChildIDs[0])
	}
}

func TestSessionNestedChildren(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionStore(dir)

	root, _ := store.Create("orchestrator", "Root", "")
	child, _ := store.SpawnChild(root.ID, "coding", "Code task")
	grandchild, _ := store.SpawnChild(child.ID, "research", "Research subtask")

	// Grandchild should have root's ID as RootID
	if grandchild.RootID != root.ID {
		t.Errorf("grandchild.RootID = %q, want %q", grandchild.RootID, root.ID)
	}
	if grandchild.ParentID != child.ID {
		t.Errorf("grandchild.ParentID = %q, want %q", grandchild.ParentID, child.ID)
	}
}

func TestSessionList(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionStore(dir)

	store.Create("a", "First", "")
	store.Create("b", "Second", "")

	metas, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 2 {
		t.Errorf("got %d sessions, want 2", len(metas))
	}
	// Most recent first
	if metas[0].Title != "Second" {
		t.Errorf("first = %q, want Second", metas[0].Title)
	}
}

func TestSessionUpdateStatus(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionStore(dir)

	meta, _ := store.Create("a", "Status Test", "")
	store.UpdateStatus(meta.ID, "completed")

	got, _ := store.GetMeta(meta.ID)
	if got.Status != "completed" {
		t.Errorf("Status = %q, want completed", got.Status)
	}
}

func TestSessionDelete(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionStore(dir)

	meta, _ := store.Create("a", "Delete Me", "")
	store.Delete(meta.ID)

	_, err := store.GetMeta(meta.ID)
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestSessionArchive(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionStore(dir)

	meta, _ := store.Create("a", "Archive Me", "")
	store.Append(meta.ID, EventUserMessage, MessageData{Content: "test"})

	err := store.Archive(meta.ID)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// Should not be in list anymore
	metas, _ := store.List()
	for _, m := range metas {
		if m.ID == meta.ID {
			t.Error("archived session should not appear in list")
		}
	}
}

func TestSessionTokenTracking(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionStore(dir)

	meta, _ := store.Create("a", "Tokens", "")
	store.Append(meta.ID, EventStepFinish, StepData{TokensIn: 100, TokensOut: 50})
	store.Append(meta.ID, EventStepFinish, StepData{TokensIn: 200, TokensOut: 75})

	got, _ := store.GetMeta(meta.ID)
	if got.TokensIn != 300 {
		t.Errorf("TokensIn = %d, want 300", got.TokensIn)
	}
	if got.TokensOut != 125 {
		t.Errorf("TokensOut = %d, want 125", got.TokensOut)
	}
}

func TestSessionListEmpty(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionStore(dir)

	metas, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 0 {
		t.Errorf("got %d, want 0", len(metas))
	}
}

func TestSessionUpdateTitle(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionStore(dir)

	meta, _ := store.Create("a", "Original Title", "")
	err := store.UpdateTitle(meta.ID, "New Title")
	if err != nil {
		t.Fatalf("UpdateTitle: %v", err)
	}

	got, _ := store.GetMeta(meta.ID)
	if got.Title != "New Title" {
		t.Errorf("Title = %q, want %q", got.Title, "New Title")
	}
}

func TestSessionLock(t *testing.T) {
	lock := newSessionLock()

	m1 := lock.For("sess-1")
	m2 := lock.For("sess-1")
	m3 := lock.For("sess-2")

	if m1 != m2 {
		t.Error("same session should return same mutex")
	}
	if m1 == m3 {
		t.Error("different sessions should return different mutexes")
	}
}

func TestGetOrCreateMain_Idempotent(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionStore(dir)

	m1, err := store.GetOrCreateMain("orchestrator")
	if err != nil {
		t.Fatal(err)
	}
	if m1.ID != MainSessionID {
		t.Fatalf("expected ID %q, got %q", MainSessionID, m1.ID)
	}
	if m1.Kind != "main" {
		t.Fatalf("expected kind 'main', got %q", m1.Kind)
	}

	// Second call returns same session
	m2, err := store.GetOrCreateMain("orchestrator")
	if err != nil {
		t.Fatal(err)
	}
	if m2.ID != m1.ID {
		t.Fatalf("expected same ID %q, got %q", m1.ID, m2.ID)
	}
}

func TestGetOrCreateMain_Persists(t *testing.T) {
	dir := t.TempDir()
	store1 := NewSessionStore(dir)
	_, err := store1.GetOrCreateMain("orchestrator")
	if err != nil {
		t.Fatal(err)
	}

	// New store instance, same dir
	store2 := NewSessionStore(dir)
	m, err := store2.GetOrCreateMain("orchestrator")
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != MainSessionID {
		t.Fatalf("expected persisted main session, got ID %q", m.ID)
	}
	if m.Kind != "main" {
		t.Fatalf("expected kind 'main' after reload, got %q", m.Kind)
	}
}

func TestCreateProject(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionStore(dir)

	m1, err := store.CreateProject("orchestrator", "/home/user/src/myproject")
	if err != nil {
		t.Fatal(err)
	}
	if m1.Kind != "project" {
		t.Fatalf("expected kind 'project', got %q", m1.Kind)
	}
	if m1.ProjectRoot != "/home/user/src/myproject" {
		t.Fatalf("expected project root, got %q", m1.ProjectRoot)
	}
	if m1.Title != "myproject" {
		t.Fatalf("expected title 'myproject', got %q", m1.Title)
	}

	// Second call returns same session (idempotent)
	m2, err := store.CreateProject("orchestrator", "/home/user/src/myproject")
	if err != nil {
		t.Fatal(err)
	}
	if m2.ID != m1.ID {
		t.Fatalf("expected same session, got different IDs: %q vs %q", m1.ID, m2.ID)
	}
}

func TestFindByProject(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionStore(dir)

	// No sessions yet
	if found := store.FindByProject("/nonexistent"); found != nil {
		t.Fatal("expected nil for nonexistent project")
	}

	store.CreateProject("orchestrator", "/home/user/src/eclaire")

	found := store.FindByProject("/home/user/src/eclaire")
	if found == nil {
		t.Fatal("expected to find project session")
	}
	if found.ProjectRoot != "/home/user/src/eclaire" {
		t.Fatalf("expected matching root, got %q", found.ProjectRoot)
	}

	// Different project root should not match
	if other := store.FindByProject("/home/user/src/other"); other != nil {
		t.Fatal("should not match different project root")
	}
}

func TestSavePatterns(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionStore(dir)

	meta, _ := store.Create("coding", "test", "")
	patterns := map[string][]string{
		"coding:shell": {"git *", "npm *"},
	}
	if err := store.SavePatterns(meta.ID, patterns); err != nil {
		t.Fatal(err)
	}

	// Reload and verify
	loaded, err := store.GetMeta(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.ApprovalPatterns) == 0 {
		t.Fatal("expected approval patterns after save")
	}
	if len(loaded.ApprovalPatterns["coding:shell"]) != 2 {
		t.Fatalf("expected 2 patterns, got %d", len(loaded.ApprovalPatterns["coding:shell"]))
	}
}
