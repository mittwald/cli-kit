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

func TestAgo(t *testing.T) {
	now := time.Date(2026, 3, 14, 12, 0, 0, 0, time.UTC)
	const day = 24 * time.Hour
	tests := []struct {
		ago  time.Duration
		want string
	}{
		{0, "now"},
		{500 * time.Millisecond, "now"},
		{time.Second, "a second ago"},
		{59 * time.Second, "59 seconds ago"},
		{time.Minute, "a minute ago"},
		{119 * time.Second, "a minute ago"},
		{2 * time.Minute, "2 minutes ago"},
		{59 * time.Minute, "59 minutes ago"},
		{time.Hour, "an hour ago"},
		{2*time.Hour - time.Second, "an hour ago"},
		{2 * time.Hour, "2 hours ago"},
		{23 * time.Hour, "23 hours ago"},
		{day, "a day ago"},
		{30 * day, "30 days ago"},
		{31 * day, "a month ago"},
		{61 * day, "2 months ago"},
		{364 * day, "11 months ago"},
		{365 * day, "a year ago"},
		{366 * day, "1 year, 1 day ago"},
		{(365 + 20) * day, "1 year, 20 days ago"},
		{(365 + 31) * day, "1 year, 1 month ago"},
		{(365 + 100) * day, "1 year, 3 months ago"},
		{2 * 365 * day, "2 years ago"},
		{12 * 365 * day, "12 years ago"},
		{-time.Second, "a second from now"},
		{-3 * time.Hour, "3 hours from now"},
		{-48 * time.Hour, "2 days from now"},
	}
	for _, tt := range tests {
		if got := Ago(now.Add(-tt.ago), now); got != tt.want {
			t.Errorf("Ago(now - %v) = %q, want %q", tt.ago, got, tt.want)
		}
	}
	// Further than a duration reaches is still ahead, and still years.
	if got := Ago(time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC), now); got != "292 years from now" {
		t.Errorf("Ago(year 9999) = %q", got)
	}
	if got := Ago(time.Time{}, now); got != "" {
		t.Errorf("Ago(zero time) = %q, want \"\"", got)
	}
}

func TestParseTimestamp(t *testing.T) {
	want := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	for text, ok := range map[string]bool{
		"2026-11-16T12:00:00Z":      true,
		"2026-11-16T13:00:00+01:00": true,
		"2026-11-16T13:00:00+0100":  true,
		"2026-11-16T12:00:00.000Z":  true,
		"2026-11-16":                false,
		"2026-11-16T12:00Z":         false,
		"2026-11-16T12:00:00":       false,
		"2026-11-16 12:00:00Z":      false,
		"2026-13-16T12:00:00Z":      false,
		"due 2026-11-16T12:00:00Z":  false,
	} {
		got, parsed := ParseTimestamp(text)
		if parsed != ok || ok && !got.Equal(want) {
			t.Errorf("ParseTimestamp(%q) = %v, %v", text, got, parsed)
		}
	}
}
