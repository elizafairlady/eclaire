package agent

import (
	"strconv"
	"strings"
	"time"
)

// cronMatches checks if a 5-field cron expression matches the given time.
// Format: minute hour day-of-month month day-of-week
func cronMatches(schedule string, t time.Time) bool {
	fields := strings.Fields(schedule)
	if len(fields) != 5 {
		return false
	}

	checks := []struct {
		field string
		value int
	}{
		{fields[0], t.Minute()},
		{fields[1], t.Hour()},
		{fields[2], t.Day()},
		{fields[3], int(t.Month())},
		{fields[4], int(t.Weekday())},
	}

	for _, c := range checks {
		if !cronFieldMatches(c.field, c.value) {
			return false
		}
	}
	return true
}

func cronFieldMatches(field string, value int) bool {
	if field == "*" {
		return true
	}

	// Handle ranges like "1-5"
	if strings.Contains(field, "-") && !strings.Contains(field, ",") {
		parts := strings.SplitN(field, "-", 2)
		lo, _ := strconv.Atoi(parts[0])
		hi, _ := strconv.Atoi(parts[1])
		return value >= lo && value <= hi
	}

	// Handle intervals like "*/5"
	if strings.HasPrefix(field, "*/") {
		interval, _ := strconv.Atoi(strings.TrimPrefix(field, "*/"))
		if interval <= 0 {
			return false
		}
		return value%interval == 0
	}

	// Handle comma-separated values like "0,15,30,45"
	for _, part := range strings.Split(field, ",") {
		v, _ := strconv.Atoi(strings.TrimSpace(part))
		if v == value {
			return true
		}
	}

	return false
}
