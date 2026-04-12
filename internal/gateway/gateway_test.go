package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elizafairlady/eclaire/internal/config"
)

func setupTestGateway(t *testing.T) (*Gateway, *config.Store, string) {
	t.Helper()

	tmpDir := t.TempDir()
	eclaireDir := filepath.Join(tmpDir, ".eclaire")
	os.MkdirAll(filepath.Join(eclaireDir, "agents"), 0o700)
	os.MkdirAll(filepath.Join(eclaireDir, "sessions"), 0o700)
	os.MkdirAll(filepath.Join(eclaireDir, "logs"), 0o700)
	os.MkdirAll(filepath.Join(eclaireDir, "credentials"), 0o700)
	os.MkdirAll(filepath.Join(eclaireDir, "cache"), 0o700)

	socketPath := filepath.Join(eclaireDir, "test.sock")

	// Write config
	configYAML := `gateway:
  idle_timeout: "1m"
  log_level: "debug"
  socket_path: "` + socketPath + `"
`
	os.WriteFile(filepath.Join(eclaireDir, "config.yaml"), []byte(configYAML), 0o644)

	// Write a test agent
	agentYAML := `id: test-agent
name: "Test Agent"
role: simple
bindings:
  - type: task
    pattern: "*"
`
	os.WriteFile(filepath.Join(eclaireDir, "agents", "test.yaml"), []byte(agentYAML), 0o644)

	// Trick config.Load by setting HOME
	t.Setenv("HOME", tmpDir)
	store, err := config.Load(tmpDir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	gw, err := New(store, logger)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return gw, store, eclaireDir
}

func connectWithRetry(t *testing.T, socketPath string) *Client {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		client, err := Connect(socketPath)
		if err == nil {
			return client
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("failed to connect to gateway")
	return nil
}

func TestGatewayStartAndConnect(t *testing.T) {
	gw, store, _ := setupTestGateway(t)

	go gw.Start()
	t.Cleanup(func() { gw.Shutdown() })

	client := connectWithRetry(t, store.SocketPath())
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	data, err := client.Call(ctx, MethodGatewayStatus, nil)
	if err != nil {
		t.Fatalf("Call status: %v", err)
	}

	var status GatewayStatus
	if err := json.Unmarshal(data, &status); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if status.PID == 0 {
		t.Error("PID should not be zero")
	}
	// 5 built-in + 1 test-agent from YAML in temp dir = 6
	if status.ActiveAgents != 6 {
		t.Errorf("ActiveAgents = %d, want 6 (5 built-in + 1 test YAML agent)", status.ActiveAgents)
	}
	if status.MainSessionID == "" {
		t.Error("MainSessionID should be set on startup")
	}
}

func TestGatewayAgentList(t *testing.T) {
	gw, store, _ := setupTestGateway(t)

	go gw.Start()
	t.Cleanup(func() { gw.Shutdown() })

	client := connectWithRetry(t, store.SocketPath())
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	data, err := client.Call(ctx, MethodAgentList, nil)
	if err != nil {
		t.Fatalf("Call agent.list: %v", err)
	}

	var agents []json.RawMessage
	if err := json.Unmarshal(data, &agents); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// 5 built-in + 1 test-agent from YAML
	if len(agents) != 6 {
		t.Errorf("got %d agents, want 6 (5 built-in + 1 test YAML)", len(agents))
	}
	// Verify specific built-in IDs are present
	agentIDs := make(map[string]bool)
	for _, raw := range agents {
		var info struct{ ID string `json:"id"` }
		json.Unmarshal(raw, &info)
		agentIDs[info.ID] = true
	}
	for _, expected := range []string{"orchestrator", "coding", "research", "sysadmin", "config"} {
		if !agentIDs[expected] {
			t.Errorf("missing built-in agent %q", expected)
		}
	}
}

func TestGatewayUnknownMethod(t *testing.T) {
	gw, store, _ := setupTestGateway(t)

	go gw.Start()
	t.Cleanup(func() { gw.Shutdown() })

	client := connectWithRetry(t, store.SocketPath())
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Call(ctx, "nonexistent.method", nil)
	if err == nil {
		t.Fatal("expected error for unknown method")
	}
	if !strings.Contains(err.Error(), "unknown method") {
		t.Errorf("error = %q, want it to contain 'unknown method'", err.Error())
	}
}

func TestGatewayConnect(t *testing.T) {
	gw, store, eclaireDir := setupTestGateway(t)

	go gw.Start()
	t.Cleanup(func() { gw.Shutdown() })

	client := connectWithRetry(t, store.SocketPath())
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect with CWD — should get back main session ID
	data, err := client.Call(ctx, MethodConnect, ConnectRequest{CWD: eclaireDir})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	var resp ConnectResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("Unmarshal ConnectResponse: %v", err)
	}

	if resp.MainSessionID == "" {
		t.Error("MainSessionID should be set")
	}
	t.Logf("Connected: main=%s project=%s", resp.MainSessionID, resp.ProjectSessionID)
}

func TestGatewaySessionList(t *testing.T) {
	gw, store, _ := setupTestGateway(t)

	go gw.Start()
	t.Cleanup(func() { gw.Shutdown() })

	client := connectWithRetry(t, store.SocketPath())
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	data, err := client.Call(ctx, MethodSessionList, nil)
	if err != nil {
		t.Fatalf("SessionList: %v", err)
	}

	// Should have at least the main session
	var sessions []json.RawMessage
	json.Unmarshal(data, &sessions)
	if len(sessions) < 1 {
		t.Errorf("got %d sessions, want at least 1 (main)", len(sessions))
	}
	t.Logf("Sessions: %d", len(sessions))
}

func TestGatewayNotificationList(t *testing.T) {
	gw, store, _ := setupTestGateway(t)

	go gw.Start()
	t.Cleanup(func() { gw.Shutdown() })

	client := connectWithRetry(t, store.SocketPath())
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// List with no notifications — should return empty array
	data, err := client.Call(ctx, MethodNotificationList, nil)
	if err != nil {
		t.Fatalf("NotificationList: %v", err)
	}

	if string(data) != "[]" {
		t.Logf("Notifications: %s", string(data))
	}
}

func TestGatewayJobList(t *testing.T) {
	gw, store, _ := setupTestGateway(t)

	go gw.Start()
	t.Cleanup(func() { gw.Shutdown() })

	client := connectWithRetry(t, store.SocketPath())
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	data, err := client.Call(ctx, MethodJobList, nil)
	if err != nil {
		t.Fatalf("JobList: %v", err)
	}

	// Should have dreaming jobs (created disabled on startup)
	var jobs []json.RawMessage
	json.Unmarshal(data, &jobs)
	t.Logf("Jobs: %d", len(jobs))

	// Dreaming creates 3 jobs
	if len(jobs) < 3 {
		t.Errorf("got %d jobs, want at least 3 (dreaming phases)", len(jobs))
	}
}

func TestGatewayToolList(t *testing.T) {
	gw, store, _ := setupTestGateway(t)

	go gw.Start()
	t.Cleanup(func() { gw.Shutdown() })

	client := connectWithRetry(t, store.SocketPath())
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	data, err := client.Call(ctx, MethodToolList, nil)
	if err != nil {
		t.Fatalf("ToolList: %v", err)
	}

	var tools []json.RawMessage
	json.Unmarshal(data, &tools)
	if len(tools) < 20 {
		t.Errorf("got %d tools, want at least 20", len(tools))
	}
	t.Logf("Tools: %d", len(tools))
}

func TestGatewayShutdown(t *testing.T) {
	gw, store, _ := setupTestGateway(t)

	done := make(chan struct{})
	go func() {
		gw.Start()
		close(done)
	}()

	client := connectWithRetry(t, store.SocketPath())
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Request shutdown
	client.Call(ctx, MethodGatewayShutdown, nil)

	// Gateway should stop
	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Error("gateway did not shut down in time")
		gw.Shutdown()
	}
}
