package agent

import (
	"testing"
	"time"
)

func TestCronMatches(t *testing.T) {
	// Monday 2026-04-06 09:30
	tm := time.Date(2026, 4, 6, 9, 30, 0, 0, time.Local)

	tests := []struct {
		schedule string
		want     bool
	}{
		{"30 9 * * *", true},       // exact match
		{"* * * * *", true},        // every minute
		{"0 9 * * *", false},       // minute doesn't match
		{"30 9 * * 1", true},       // Monday = 1
		{"30 9 * * 0", false},      // Sunday = 0
		{"30 9 6 4 *", true},       // 6th of April
		{"30 9 7 4 *", false},      // 7th of April
		{"*/30 * * * *", true},     // every 30 minutes (30%30==0)
		{"*/15 * * * *", true},     // every 15 minutes (30%15==0)
		{"*/7 * * * *", false},     // every 7 minutes (30%7==2)
		{"0,15,30,45 * * * *", true}, // comma list
		{"0,15,45 * * * *", false},   // not in list
		{"30 9 * * 1-5", true},     // weekday range (Mon=1)
		{"30 9 * * 6-7", false},    // weekend range
	}

	for _, tt := range tests {
		got := cronMatches(tt.schedule, tm)
		if got != tt.want {
			t.Errorf("cronMatches(%q, Mon 09:30) = %v, want %v", tt.schedule, got, tt.want)
		}
	}
}

func TestCronFieldMatches(t *testing.T) {
	tests := []struct {
		field string
		value int
		want  bool
	}{
		{"*", 0, true},
		{"*", 59, true},
		{"5", 5, true},
		{"5", 6, false},
		{"1-5", 3, true},
		{"1-5", 0, false},
		{"1-5", 6, false},
		{"*/10", 0, true},
		{"*/10", 10, true},
		{"*/10", 20, true},
		{"*/10", 5, false},
		{"0,15,30", 15, true},
		{"0,15,30", 10, false},
	}

	for _, tt := range tests {
		got := cronFieldMatches(tt.field, tt.value)
		if got != tt.want {
			t.Errorf("cronFieldMatches(%q, %d) = %v, want %v", tt.field, tt.value, got, tt.want)
		}
	}
}

func TestCronMatchesInvalidSchedule(t *testing.T) {
	tm := time.Now()
	if cronMatches("invalid", tm) {
		t.Error("invalid schedule should not match")
	}
	if cronMatches("", tm) {
		t.Error("empty schedule should not match")
	}
	if cronMatches("1 2 3", tm) {
		t.Error("3-field schedule should not match (need 5)")
	}
}
