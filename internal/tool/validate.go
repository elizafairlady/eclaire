package tool

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

const (
	// MaxInputBytes is the maximum size of a tool call's JSON input.
	MaxInputBytes = 500_000 // 500KB

	// MaxStringFieldBytes is the maximum size of any single string field in tool input.
	MaxStringFieldBytes = 200_000 // 200KB
)

// ValidateToolInput checks a tool's JSON input for structural issues and
// injection patterns. Returns nil if the input is valid, or an error describing
// the first problem found.
//
// This is defense-in-depth — individual tools also validate their inputs,
// but this catches common problems before they reach tool-specific code.
func ValidateToolInput(toolName, input string) error {
	if input == "" {
		return nil // empty input is valid (some tools take no params)
	}

	// Size check
	if len(input) > MaxInputBytes {
		return fmt.Errorf("input exceeds max size (%d > %d bytes)", len(input), MaxInputBytes)
	}

	// Must be valid JSON
	var parsed map[string]any
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		// Some tools accept non-object JSON (e.g., a plain string).
		// Only flag it if it's not valid JSON at all.
		if !json.Valid([]byte(input)) {
			return fmt.Errorf("invalid JSON input: %w", err)
		}
		return nil // valid JSON but not an object — let the tool handle it
	}

	// Check each string field
	for key, val := range parsed {
		str, ok := val.(string)
		if !ok {
			continue
		}

		// String size limit
		if len(str) > MaxStringFieldBytes {
			return fmt.Errorf("field %q exceeds max string size (%d > %d bytes)",
				key, len(str), MaxStringFieldBytes)
		}

		// Control character check (except \n, \r, \t which are normal)
		if containsDangerousControlChars(str) {
			return fmt.Errorf("field %q contains dangerous control characters", key)
		}

		// Path fields: check for null bytes (symlink and traversal handled by workspace boundary)
		if isPathField(key) && strings.ContainsRune(str, 0) {
			return fmt.Errorf("path field %q contains null byte", key)
		}
	}

	return nil
}

// containsDangerousControlChars checks for control characters that shouldn't
// appear in tool inputs (excluding newline, carriage return, tab).
func containsDangerousControlChars(s string) bool {
	for _, r := range s {
		if r != '\n' && r != '\r' && r != '\t' && unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// isPathField returns true if the field name typically contains a file path.
func isPathField(name string) bool {
	switch name {
	case "path", "file_path", "file", "cwd", "directory", "dir":
		return true
	}
	return false
}
