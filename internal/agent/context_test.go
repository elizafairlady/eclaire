package agent

import "testing"

func TestContextBudget_ShouldSummarize_SmallWindow(t *testing.T) {
	tests := []struct {
		window int64
		used   int64
		want   bool
	}{
		{100000, 0, false},
		{100000, 50000, false},
		{100000, 79999, false},
		{100000, 80001, true},  // 20% remaining = 20000, used > 80000
		{100000, 95000, true},
	}

	for _, tt := range tests {
		b := ContextBudget{ContextWindow: tt.window, UsedTokens: tt.used}
		if got := b.ShouldSummarize(); got != tt.want {
			t.Errorf("window=%d used=%d: ShouldSummarize()=%v, want %v", tt.window, tt.used, got, tt.want)
		}
	}
}

func TestContextBudget_ShouldSummarize_LargeWindow(t *testing.T) {
	tests := []struct {
		window int64
		used   int64
		want   bool
	}{
		{1000000, 0, false},
		{1000000, 500000, false},
		{1000000, 959999, false},
		{1000000, 960001, true}, // remaining < 40000
		{1000000, 999000, true},
	}

	for _, tt := range tests {
		b := ContextBudget{ContextWindow: tt.window, UsedTokens: tt.used}
		if got := b.ShouldSummarize(); got != tt.want {
			t.Errorf("window=%d used=%d: ShouldSummarize()=%v, want %v", tt.window, tt.used, got, tt.want)
		}
	}
}

func TestContextBudget_ShouldSummarize_ZeroWindow(t *testing.T) {
	b := ContextBudget{ContextWindow: 0, UsedTokens: 100}
	if b.ShouldSummarize() {
		t.Error("zero window should never trigger summarize")
	}
}

func TestContextBudget_RemainingTokens(t *testing.T) {
	b := ContextBudget{ContextWindow: 100000, UsedTokens: 30000}
	if got := b.RemainingTokens(); got != 70000 {
		t.Errorf("RemainingTokens() = %d, want 70000", got)
	}

	b2 := ContextBudget{ContextWindow: 100, UsedTokens: 200}
	if got := b2.RemainingTokens(); got != 0 {
		t.Errorf("overused RemainingTokens() = %d, want 0", got)
	}
}
