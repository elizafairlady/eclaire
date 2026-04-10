package agent

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
)

// InstructionFile is a discovered project instruction file (CLAUDE.md, etc.).
type InstructionFile struct {
	Path    string
	Content string
}

// DiscoverInstructionFiles walks from cwd to root, collecting instruction files.
// Files are collected in root-first order. Duplicates (by content hash) are removed.
// Reference: Claw Code rust/crates/runtime/src/prompt.rs discover_instruction_files()
func DiscoverInstructionFiles(cwd string) []InstructionFile {
	// Build ancestor chain from root to cwd
	var dirs []string
	cursor := cwd
	for {
		dirs = append(dirs, cursor)
		parent := filepath.Dir(cursor)
		if parent == cursor {
			break
		}
		cursor = parent
	}
	// Reverse so we walk root → cwd
	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}

	var files []InstructionFile
	for _, dir := range dirs {
		candidates := []string{
			filepath.Join(dir, "CLAUDE.md"),
			filepath.Join(dir, "CLAUDE.local.md"),
			filepath.Join(dir, ".eclaire", "CLAUDE.md"),
			filepath.Join(dir, ".eclaire", "instructions.md"),
		}
		for _, path := range candidates {
			content, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			text := strings.TrimSpace(string(content))
			if text == "" {
				continue
			}
			files = append(files, InstructionFile{
				Path:    path,
				Content: text,
			})
		}
	}

	return dedupeInstructionFiles(files)
}

// dedupeInstructionFiles removes files with identical content (by SHA-256 hash).
// Keeps the first occurrence (closest to root).
func dedupeInstructionFiles(files []InstructionFile) []InstructionFile {
	seen := make(map[[32]byte]bool, len(files))
	var result []InstructionFile
	for _, f := range files {
		normalized := strings.TrimSpace(f.Content)
		hash := sha256.Sum256([]byte(normalized))
		if seen[hash] {
			continue
		}
		seen[hash] = true
		result = append(result, f)
	}
	return result
}
