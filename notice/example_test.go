package notice_test

import (
	"errors"
	"os"

	"github.com/mittwald/cli-kit/notice"
)

func Example() {
	// os.Stdout is no terminal in a test, so the notices come out plain.
	p := notice.New(notice.Options{Theme: notice.Badge, Writer: os.Stdout})
	p.Warnf("Booking day %s was never left: the start at %s has no Gehen", notice.Date("19.11.2026"), notice.Time("08:40"))
	p.Error(errors.New("request timed out: password=hunter2"))
	p.Debugf("not shown without Options.Debug")
	// Output:
	// WARN: Booking day 19.11.2026 was never left: the start at 08:40 has no Gehen
	// ERROR: request timed out: password=[REDACTED]
}
