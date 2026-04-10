package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
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
	if status.ActiveAgents < 5 {
		t.Errorf("ActiveAgents = %d, want at least 5 (built-ins)", status.ActiveAgents)
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

	if len(agents) < 5 {
		t.Errorf("got %d agents, want at least 5", len(agents))
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
		t.Error("expected error for unknown method")
	}
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
