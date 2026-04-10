package tool

import (
	"testing"
)

func TestParseHunks(t *testing.T) {
	patch := `--- a/file.txt
+++ b/file.txt
@@ -1,3 +1,4 @@
 line1
+inserted
 line2
 line3
`
	hunks, err := parseHunks(patch)
	if err != nil {
		t.Fatalf("parseHunks: %v", err)
	}
	if len(hunks) != 1 {
		t.Fatalf("got %d hunks, want 1", len(hunks))
	}
	if hunks[0].oldStart != 1 {
		t.Errorf("oldStart = %d, want 1", hunks[0].oldStart)
	}
}

func TestApplyHunks(t *testing.T) {
	original := []string{"line1", "line2", "line3"}
	hunks := []hunk{
		{
			oldStart: 1,
			oldCount: 3,
			newStart: 1,
			newCount: 4,
			lines: []diffLine{
				{op: ' ', text: "line1"},
				{op: '+', text: "inserted"},
				{op: ' ', text: "line2"},
				{op: ' ', text: "line3"},
			},
		},
	}

	result, err := applyHunks(original, hunks)
	if err != nil {
		t.Fatalf("applyHunks: %v", err)
	}

	if len(result) != 4 {
		t.Fatalf("got %d lines, want 4: %v", len(result), result)
	}
	if result[0] != "line1" {
		t.Errorf("line 0 = %q", result[0])
	}
	if result[1] != "inserted" {
		t.Errorf("line 1 = %q, want 'inserted'", result[1])
	}
	if result[2] != "line2" {
		t.Errorf("line 2 = %q", result[2])
	}
}

func TestApplyHunksDeletion(t *testing.T) {
	original := []string{"line1", "line2", "line3"}
	hunks := []hunk{
		{
			oldStart: 1,
			oldCount: 3,
			newStart: 1,
			newCount: 2,
			lines: []diffLine{
				{op: ' ', text: "line1"},
				{op: '-', text: "line2"},
				{op: ' ', text: "line3"},
			},
		},
	}

	result, err := applyHunks(original, hunks)
	if err != nil {
		t.Fatalf("applyHunks: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("got %d lines, want 2: %v", len(result), result)
	}
	if result[0] != "line1" || result[1] != "line3" {
		t.Errorf("result = %v", result)
	}
}
