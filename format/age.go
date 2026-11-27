package format

import (
	"regexp"
	"strconv"
	"time"
)

// Age returns how long before now t was, in the compact form a column of a
// listing shows: "now" under a minute, then whole minutes ("5m"), hours
// ("3h"), days ("2d"), months of 30 days ("5mo") and years of 365 days
// ("2y"), each rounded down. A t after now is "now" too, so a clock that is
// a little off never shows a negative age. The zero time is "": an age that
// is not known leaves its cell empty.
func Age(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	const day = 24 * time.Hour
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < day:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	case d < 30*day:
		return strconv.Itoa(int(d/day)) + "d"
	case d < 365*day:
		return strconv.Itoa(int(d/(30*day))) + "mo"
	}
	return strconv.Itoa(int(d/(365*day))) + "y"
}

// Ago returns how long before now t was, or how long after, in the words
// a person reads: "now", "5 minutes ago", "a day ago", "1 year, 2 months
// ago", "3 hours from now". Months count as 30.5 days and years as 365
// days, each rounded down, so the wording is rough on purpose. Unlike
// [Age] it names a time ahead as such. The zero time is "".
func Ago(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	// The direction is decided first: a time far enough ahead makes
	// now.Sub(t) the shortest duration, which has no negation.
	distance, suffix := now.Sub(t), " ago"
	if t.After(now) {
		distance, suffix = t.Sub(now), " from now"
	}
	span := span(distance)
	if span == "" {
		return "now"
	}
	return span + suffix
}

// span words a length of time the way people say it, "" for less than a
// second.
func span(distance time.Duration) string {
	total := int64(distance / time.Second)
	days, seconds := total/86400, total%86400
	years, days := days/365, days%365
	months := int64(float64(days) / 30.5)

	count := func(n int64, one, many string) string {
		if n == 1 {
			return one
		}
		return strconv.FormatInt(n, 10) + " " + many
	}
	switch {
	case years == 0 && days < 1:
		switch {
		case seconds == 0:
			return ""
		case seconds < 60:
			return count(seconds, "a second", "seconds")
		case seconds < 120:
			return "a minute"
		case seconds < 3600:
			return strconv.FormatInt(seconds/60, 10) + " minutes"
		case seconds < 7200:
			return "an hour"
		}
		return strconv.FormatInt(seconds/3600, 10) + " hours"
	case years == 0:
		if months == 0 {
			return count(days, "a day", "days")
		}
		return count(months, "a month", "months")
	case years == 1:
		switch {
		case months == 0 && days == 0:
			return "a year"
		case months == 0:
			return "1 year, " + count(days, "1 day", "days")
		}
		return "1 year, " + count(months, "1 month", "months")
	}
	return strconv.FormatInt(years, 10) + " years"
}

// timestamp matches a point in time as APIs write it: a date and a time of
// day, with seconds and a zone. time.Parse takes any two digits for the
// hours and minutes of an offset, so the pattern holds them in range.
var timestamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]([01]\d|2[0-3]):?([0-5]\d))$`)

// timestampLayouts are the spellings of the zone that timestamp admits.
var timestampLayouts = []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999Z0700"}

// ParseTimestamp reads text as a point in time the way APIs write one:
// RFC 3339 with seconds and a zone, such as "2026-11-16T12:00:00Z" or
// "2026-11-16T13:00:00.5+01:00", or with the zone written without its
// colon, "+0100". It reports false for anything else, a date alone
// included, so it tells the cells of a listing that hold a time from those
// that do not.
func ParseTimestamp(text string) (time.Time, bool) {
	if !timestamp.MatchString(text) {
		return time.Time{}, false
	}
	for _, layout := range timestampLayouts {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
