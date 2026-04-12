package tool

import (
	"strings"
	"testing"
)

func TestValidateToolInputEmpty(t *testing.T) {
	if err := ValidateToolInput("shell", ""); err != nil {
		t.Errorf("empty input should be valid: %v", err)
	}
}

func TestValidateToolInputValidJSON(t *testing.T) {
	if err := ValidateToolInput("shell", `{"command":"echo hello"}`); err != nil {
		t.Errorf("valid JSON should pass: %v", err)
	}
}

func TestValidateToolInputInvalidJSON(t *testing.T) {
	if err := ValidateToolInput("shell", `{broken json`); err == nil {
		t.Error("invalid JSON should fail")
	}
}

func TestValidateToolInputOversized(t *testing.T) {
	huge := `{"command":"` + strings.Repeat("x", MaxInputBytes) + `"}`
	if err := ValidateToolInput("shell", huge); err == nil {
		t.Error("oversized input should fail")
	}
}

func TestValidateToolInputOversizedStringField(t *testing.T) {
	big := `{"content":"` + strings.Repeat("a", MaxStringFieldBytes+1) + `"}`
	if err := ValidateToolInput("write", big); err == nil {
		t.Error("oversized string field should fail")
	}
}

func TestValidateToolInputControlChars(t *testing.T) {
	// Null byte in a path field
	if err := ValidateToolInput("write", `{"path":"file\u0000.txt"}`); err == nil {
		t.Error("null byte in path should fail")
	}

	// Bell character (control char) in a string field
	if err := ValidateToolInput("shell", `{"command":"echo \u0007"}`); err == nil {
		t.Error("control character should fail")
	}
}

func TestValidateToolInputAllowsNewlines(t *testing.T) {
	// Newlines and tabs are normal in content
	if err := ValidateToolInput("write", `{"content":"line1\nline2\ttab"}`); err != nil {
		t.Errorf("newlines/tabs should be allowed: %v", err)
	}
}

func TestValidateToolInputNonObjectJSON(t *testing.T) {
	// Some tools might receive non-object JSON (e.g., a string)
	if err := ValidateToolInput("shell", `"just a string"`); err != nil {
		t.Errorf("non-object valid JSON should pass: %v", err)
	}
}

func TestContainsDangerousControlChars(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"normal text", false},
		{"with\nnewline", false},
		{"with\ttab", false},
		{"with\rcarriage return", false},
		{"with\x00null", true},
		{"with\x07bell", true},
		{"with\x1bescape", true},
	}
	for _, tt := range tests {
		got := containsDangerousControlChars(tt.s)
		if got != tt.want {
			t.Errorf("containsDangerousControlChars(%q) = %v, want %v", tt.s, got, tt.want)
		}
	}
}

func TestIsPathField(t *testing.T) {
	pathFields := []string{"path", "file_path", "file", "cwd", "directory", "dir"}
	for _, f := range pathFields {
		if !isPathField(f) {
			t.Errorf("isPathField(%q) should be true", f)
		}
	}
	nonPathFields := []string{"command", "content", "prompt", "text"}
	for _, f := range nonPathFields {
		if isPathField(f) {
			t.Errorf("isPathField(%q) should be false", f)
		}
	}
}
