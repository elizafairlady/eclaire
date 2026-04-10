package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

// Client connects to the gateway daemon.
type Client struct {
	conn    net.Conn
	encoder *json.Encoder
	decoder *json.Decoder

	pending map[string]chan Envelope
	events  chan Envelope
	mu      sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
}

// Connect dials the gateway Unix socket.
func Connect(socketPath string) (*Client, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		conn:    conn,
		encoder: json.NewEncoder(conn),
		decoder: json.NewDecoder(conn),
		pending: make(map[string]chan Envelope),
		events:  make(chan Envelope, 64),
		ctx:     ctx,
		cancel:  cancel,
	}
	go c.readLoop()
	return c, nil
}

// EnsureGateway connects to an existing gateway or starts one.
func EnsureGateway(socketPath, pidPath string) (*Client, error) {
	// Try connecting to existing gateway
	client, err := Connect(socketPath)
	if err == nil {
		return client, nil
	}

	// Check for stale PID file
	if data, err := os.ReadFile(pidPath); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			if proc, err := os.FindProcess(pid); err == nil {
				if proc.Signal(syscall.Signal(0)) != nil {
					// Process is dead, clean up
					os.Remove(socketPath)
					os.Remove(pidPath)
				}
			}
		}
	}

	// Start new daemon
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("find executable: %w", err)
	}

	logDir := filepath.Dir(pidPath)
	logFile := filepath.Join(logDir, "logs", "gateway.log")
	os.MkdirAll(filepath.Dir(logFile), 0o700)

	out, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log: %w", err)
	}

	cmd := exec.Command(exe, "--daemon")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		out.Close()
		return nil, fmt.Errorf("start daemon: %w", err)
	}
	out.Close()

	// Detach - don't wait for daemon
	go cmd.Wait()

	// Poll for socket
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		client, err = Connect(socketPath)
		if err == nil {
			return client, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, fmt.Errorf("gateway failed to start within 3s")
}

// Call sends a request and waits for the final response, ignoring stream parts.
func (c *Client) Call(ctx context.Context, method string, data any) (json.RawMessage, error) {
	ch, err := c.Stream(ctx, method, data)
	if err != nil {
		return nil, err
	}

	// Drain stream parts, wait for final response
	for env := range ch {
		if env.Type == TypeResponse {
			if env.Error != nil {
				return nil, fmt.Errorf("gateway: %s", env.Error.Message)
			}
			return env.Data, nil
		}
	}

	return nil, fmt.Errorf("connection closed before response")
}

// Events returns the channel for server-pushed events.
func (c *Client) Events() <-chan Envelope {
	return c.events
}

// Stream sends a request and returns a channel that receives stream parts
// followed by the final response (Type==TypeResponse).
func (c *Client) Stream(ctx context.Context, method string, data any) (<-chan Envelope, error) {
	id := uuid.NewString()

	var rawData json.RawMessage
	if data != nil {
		var err error
		rawData, err = json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("marshal: %w", err)
		}
	}

	env := Envelope{
		ID:     id,
		Type:   TypeRequest,
		Method: method,
		Data:   rawData,
	}

	out := make(chan Envelope, 64)

	// Register both pending (for final response) and stream handler
	c.mu.Lock()
	c.pending[id] = out
	c.mu.Unlock()

	if err := c.encoder.Encode(env); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		close(out)
		return nil, fmt.Errorf("send: %w", err)
	}

	return out, nil
}

// Close closes the client connection.
func (c *Client) Close() error {
	c.cancel()
	return c.conn.Close()
}

func (c *Client) readLoop() {
	for {
		var env Envelope
		if err := c.decoder.Decode(&env); err != nil {
			select {
			case <-c.ctx.Done():
			default:
				// Connection broken
			}
			return
		}

		switch env.Type {
		case TypeResponse:
			c.mu.Lock()
			ch, ok := c.pending[env.ID]
			if ok {
				ch <- env
				delete(c.pending, env.ID)
				close(ch)
			}
			c.mu.Unlock()
		case TypeStream:
			c.mu.Lock()
			if ch, ok := c.pending[env.ID]; ok {
				select {
				case ch <- env:
				default:
				}
			}
			c.mu.Unlock()
		case TypeEvent:
			select {
			case c.events <- env:
			default:
			}
		}
	}
}

// ConnectWithCWD sends the client's working directory to the gateway
// and receives session context (main session ID, project session if applicable).
func (c *Client) ConnectWithCWD(ctx context.Context, cwd string) (*ConnectResponse, error) {
	req := ConnectRequest{CWD: cwd}
	reqData, _ := json.Marshal(req)
	data, err := c.Call(ctx, MethodConnect, reqData)
	if err != nil {
		return nil, err
	}
	var resp ConnectResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse connect response: %w", err)
	}
	return &resp, nil
}
