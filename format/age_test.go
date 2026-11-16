package format

import (
	"testing"
	"time"
)

func TestAge(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	const day = 24 * time.Hour
	tests := []struct {
		name string
		ago  time.Duration
		want string
	}{
		{"just now", 0, "now"},
		{"seconds", 59 * time.Second, "now"},
		{"in the future", -time.Hour, "now"},
		{"one minute", time.Minute, "1m"},
		{"minutes round down", 59*time.Minute + 59*time.Second, "59m"},
		{"one hour", time.Hour, "1h"},
		{"hours", 23 * time.Hour, "23h"},
		{"one day", day, "1d"},
		{"days", 29 * day, "29d"},
		{"one month", 30 * day, "1mo"},
		{"months", 364 * day, "12mo"},
		{"one year", 365 * day, "1y"},
		{"years", 3*365*day + 100*day, "3y"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Age(now.Add(-tt.ago), now); got != tt.want {
				t.Errorf("Age(now - %v) = %q, want %q", tt.ago, got, tt.want)
			}
		})
	}
	if got := Age(time.Time{}, now); got != "" {
		t.Errorf("Age(zero time) = %q, want \"\"", got)
	}
}
