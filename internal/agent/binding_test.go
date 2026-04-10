package agent

import "testing"

func TestBindingMatchProject(t *testing.T) {
	b := Binding{Type: BindProject, Pattern: "/home/user/src/myproject"}

	tests := []struct {
		cwd  string
		want bool
	}{
		{"/home/user/src/myproject", true},
		{"/home/user/src/myproject/subdir", true},
		{"/home/user/src/other", false},
		{"/tmp", false},
	}

	for _, tt := range tests {
		if got := b.Match(tt.cwd, ""); got != tt.want {
			t.Errorf("Match(%q) = %v, want %v", tt.cwd, got, tt.want)
		}
	}
}

func TestBindingMatchDirectory(t *testing.T) {
	b := Binding{Type: BindDirectory, Pattern: "/home/user/src"}

	if !b.Match("/home/user/src", "") {
		t.Error("exact match should succeed")
	}
	if b.Match("/home/user/src/sub", "") {
		t.Error("subdirectory should not match BindDirectory")
	}
}

func TestBindingMatchGlob(t *testing.T) {
	b := Binding{Type: BindGlob, Pattern: "*.go"}

	if !b.Match("/some/path/main.go", "") {
		t.Error("*.go should match main.go")
	}
	if b.Match("/some/path/main.rs", "") {
		t.Error("*.go should not match main.rs")
	}
}

func TestBindingMatchTask(t *testing.T) {
	b := Binding{Type: BindTask, Pattern: "review"}

	if !b.Match("", "review") {
		t.Error("should match task type 'review'")
	}
	if b.Match("", "deploy") {
		t.Error("should not match task type 'deploy'")
	}

	// Wildcard task
	bWild := Binding{Type: BindTask, Pattern: "*"}
	if !bWild.Match("", "anything") {
		t.Error("wildcard task should match anything")
	}
}

func TestBindingUnknownType(t *testing.T) {
	b := Binding{Type: "unknown", Pattern: "whatever"}
	if b.Match("/any/path", "any-task") {
		t.Error("unknown binding type should never match")
	}
}
