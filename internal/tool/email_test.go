package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

func newTestToolCall(input string) fantasy.ToolCall {
	return fantasy.ToolCall{Input: input}
}

func TestLoadEmailCredentials(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "email.yaml")

	content := `
imap_host: imap.example.com
imap_port: 993
smtp_host: smtp.example.com
smtp_port: 587
user: alice@example.com
password: secret123
`
	os.WriteFile(path, []byte(content), 0o644)

	creds, err := LoadEmailCredentials(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if creds.IMAPHost != "imap.example.com" {
		t.Errorf("IMAPHost = %q", creds.IMAPHost)
	}
	if creds.IMAPPort != 993 {
		t.Errorf("IMAPPort = %d", creds.IMAPPort)
	}
	if creds.SMTPHost != "smtp.example.com" {
		t.Errorf("SMTPHost = %q", creds.SMTPHost)
	}
	if creds.User != "alice@example.com" {
		t.Errorf("User = %q", creds.User)
	}
	if creds.Password != "secret123" {
		t.Errorf("Password = %q", creds.Password)
	}
	// From defaults to User
	if creds.From != "alice@example.com" {
		t.Errorf("From = %q, want %q", creds.From, "alice@example.com")
	}
}

func TestLoadEmailCredentialsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "email.yaml")

	content := `
imap_host: imap.example.com
smtp_host: smtp.example.com
user: bob@example.com
password: pass
from: "Bob <bob@example.com>"
`
	os.WriteFile(path, []byte(content), 0o644)

	creds, err := LoadEmailCredentials(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if creds.IMAPPort != 993 {
		t.Errorf("default IMAPPort = %d, want 993", creds.IMAPPort)
	}
	if creds.SMTPPort != 587 {
		t.Errorf("default SMTPPort = %d, want 587", creds.SMTPPort)
	}
	if creds.From != "Bob <bob@example.com>" {
		t.Errorf("From = %q", creds.From)
	}
}

func TestLoadEmailCredentialsMissing(t *testing.T) {
	_, err := LoadEmailCredentials("/nonexistent/email.yaml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestFormatAddress(t *testing.T) {
	tests := []struct {
		addr imap.Address
		want string
	}{
		{imap.Address{Mailbox: "alice", Host: "example.com"}, "alice@example.com"},
		{imap.Address{Name: "Alice", Mailbox: "alice", Host: "example.com"}, "Alice <alice@example.com>"},
	}
	for _, tt := range tests {
		got := formatAddress(tt.addr)
		if got != tt.want {
			t.Errorf("formatAddress(%+v) = %q, want %q", tt.addr, got, tt.want)
		}
	}
}

func TestFormatMessage(t *testing.T) {
	msg := EmailMessage{
		UID:     42,
		From:    "alice@example.com",
		Subject: "Hello World",
		Date:    time.Date(2026, 4, 8, 10, 0, 0, 0, time.UTC),
		Snippet: "This is a test message...",
		Flags:   []string{"\\Seen"},
	}

	result := FormatMessage(msg)
	if !strings.Contains(result, "UID:42") {
		t.Error("should contain UID")
	}
	if !strings.Contains(result, "alice@example.com") {
		t.Error("should contain From")
	}
	if !strings.Contains(result, "Hello World") {
		t.Error("should contain Subject")
	}
	if !strings.Contains(result, "\\Seen") {
		t.Error("should contain flags")
	}
	if !strings.Contains(result, "This is a test") {
		t.Error("should contain snippet")
	}
}

func TestBufferToMessage(t *testing.T) {
	buf := &imapclient.FetchMessageBuffer{
		UID:   imap.UID(123),
		Flags: []imap.Flag{imap.FlagSeen},
		Envelope: &imap.Envelope{
			Subject: "Test Subject",
			Date:    time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC),
			From:    []imap.Address{{Name: "Alice", Mailbox: "alice", Host: "example.com"}},
			To:      []imap.Address{{Mailbox: "bob", Host: "example.com"}},
		},
	}

	em := bufferToMessage(buf)

	if em.UID != 123 {
		t.Errorf("UID = %d", em.UID)
	}
	if em.Subject != "Test Subject" {
		t.Errorf("Subject = %q", em.Subject)
	}
	if em.From != "Alice <alice@example.com>" {
		t.Errorf("From = %q", em.From)
	}
	if em.To != "bob@example.com" {
		t.Errorf("To = %q", em.To)
	}
	if len(em.Flags) != 1 || em.Flags[0] != "\\Seen" {
		t.Errorf("Flags = %v", em.Flags)
	}
}

func TestBufferToMessageNilEnvelope(t *testing.T) {
	buf := &imapclient.FetchMessageBuffer{
		UID: imap.UID(1),
	}

	em := bufferToMessage(buf)
	if em.UID != 1 {
		t.Errorf("UID = %d", em.UID)
	}
	if em.Subject != "" {
		t.Errorf("Subject should be empty, got %q", em.Subject)
	}
}

func TestEmailToolNoCredentials(t *testing.T) {
	deps := EmailDeps{CredentialsPath: "/nonexistent/email.yaml"}
	tool := EmailTool(deps)

	resp, err := tool.Run(context.Background(), newTestToolCall(`{"operation":"inbox"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Error("expected error when credentials missing")
	}
	if !strings.Contains(resp.Content, "credentials") {
		t.Errorf("error should mention credentials: %s", resp.Content)
	}
}

func TestEmailToolUnknownOperation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "email.yaml")
	os.WriteFile(path, []byte("imap_host: x\nsmtp_host: x\nuser: x\npassword: x"), 0o644)

	deps := EmailDeps{CredentialsPath: path}
	tool := EmailTool(deps)

	resp, _ := tool.Run(context.Background(), newTestToolCall(`{"operation":"unknown"}`))
	if !resp.IsError {
		t.Error("expected error for unknown operation")
	}
}

func TestEmailToolSendMissingFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "email.yaml")
	os.WriteFile(path, []byte("imap_host: x\nsmtp_host: x\nuser: x\npassword: x"), 0o644)

	deps := EmailDeps{CredentialsPath: path}
	tool := EmailTool(deps)

	// Missing to
	resp, _ := tool.Run(context.Background(), newTestToolCall(`{"operation":"send","subject":"hi","body":"hello"}`))
	if !resp.IsError {
		t.Error("expected error for missing to")
	}

	// Missing subject
	resp, _ = tool.Run(context.Background(), newTestToolCall(`{"operation":"send","to":"a@b.com","body":"hello"}`))
	if !resp.IsError {
		t.Error("expected error for missing subject")
	}

	// Missing body
	resp, _ = tool.Run(context.Background(), newTestToolCall(`{"operation":"send","to":"a@b.com","subject":"hi"}`))
	if !resp.IsError {
		t.Error("expected error for missing body")
	}
}

func TestEmailToolReadMissingUID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "email.yaml")
	os.WriteFile(path, []byte("imap_host: x\nsmtp_host: x\nuser: x\npassword: x"), 0o644)

	deps := EmailDeps{CredentialsPath: path}
	tool := EmailTool(deps)

	resp, _ := tool.Run(context.Background(), newTestToolCall(`{"operation":"read"}`))
	if !resp.IsError {
		t.Error("expected error for missing UID")
	}
}
