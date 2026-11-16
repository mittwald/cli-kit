package format

import (
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
