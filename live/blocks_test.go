package live

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/mittwald/cli-kit/theme"
)

// Detail lines come as a block with a blank line on either side, and the
// first plain line, or Close, ends it.
func TestBlocksSetDetailApart(t *testing.T) {
	var out bytes.Buffer
	blocks := NewBlocks(&out)
	fmt.Fprintln(blocks.Plain(), "Latest release: 1.4.2")
	fmt.Fprintln(blocks.Detail(), "step 1: uploading 10 files")
	fmt.Fprintln(blocks.Detail(), "step 2: uploading 4 files")
	blocks.Detail().(interface{ Close() }).Close()
	blocks.Close() // closing twice adds nothing
	fmt.Fprintln(&out, "Dry run: would publish 1.4.2")
	fmt.Fprintln(blocks.Detail(), "step 1: uploading 10 files")
	fmt.Fprintln(blocks.Plain(), "published 1.4.2")

	want := strings.Join([]string{
		"Latest release: 1.4.2",
		"",
		"step 1: uploading 10 files",
		"step 2: uploading 4 files",
		"",
		"Dry run: would publish 1.4.2",
		"",
		"step 1: uploading 10 files",
		"",
		"published 1.4.2",
		"",
	}, "\n")
	if out.String() != want {
		t.Errorf("output =\n%q\nwant\n%q", out.String(), want)
	}
}

// With color the block is muted, except for a warning.
func TestBlocksMuteAllButHighlights(t *testing.T) {
	var out bytes.Buffer
	blocks := NewBlocks(&out)
	blocks.color = true
	fmt.Fprintln(blocks.Detail(), "step 1: uploading 10 files")
	fmt.Fprintln(blocks.Detail(), "WARNING: no checksum file")

	muted := theme.Active().Status
	want := "\n" + muted.Paint("step 1: uploading 10 files") + "\nWARNING: no checksum file\n"
	if out.String() != want {
		t.Errorf("output = %q, want %q", out.String(), want)
	}
}

// A theme with a Status style paints the detail lines in it.
func TestBlocksTakeTheThemesStatusStyle(t *testing.T) {
	t.Cleanup(func() { theme.Use(theme.Default) })
	custom := theme.Default
	custom.Status = theme.Fg256(248)
	theme.Use(custom)

	var out bytes.Buffer
	blocks := NewBlocks(&out)
	blocks.color = true
	fmt.Fprintln(blocks.Detail(), "step 1")
	if want := "\n" + custom.Status.Paint("step 1") + "\n"; out.String() != want {
		t.Errorf("output = %q, want %q", out.String(), want)
	}
}

// The options replace the style and the highlight rule.
func TestBlocksOptions(t *testing.T) {
	var out bytes.Buffer
	style := theme.Fg256(33)
	blocks := NewBlocks(&out,
		WithDetailStyle(style),
		WithHighlight(func(text string) bool { return strings.HasPrefix(text, "!") }))
	blocks.color = true
	fmt.Fprintln(blocks.Detail(), "quiet")
	fmt.Fprintln(blocks.Detail(), "! loud")
	fmt.Fprintln(blocks.Detail(), "WARNING: muted now")

	want := "\n" + style.Paint("quiet") + "\n! loud\n" + style.Paint("WARNING: muted now") + "\n"
	if out.String() != want {
		t.Errorf("output = %q, want %q", out.String(), want)
	}
}

// Off a terminal nothing is painted; a buffer is never a terminal.
func TestBlocksPlainOffTerminal(t *testing.T) {
	var out bytes.Buffer
	blocks := NewBlocks(&out)
	fmt.Fprintln(blocks.Detail(), "step")
	if got, want := out.String(), "\nstep\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// A blank line written past Blocks is the one a block opening below it would
// have brought, and the one ending a block open above it: neither is
// doubled. A plain line in between uses it up.
func TestBlocksSpaced(t *testing.T) {
	var out bytes.Buffer
	blocks := NewBlocks(&out)
	fmt.Fprintln(blocks.Detail(), "step 1: uploading 10 files")
	fmt.Fprintln(&out)
	blocks.Spaced()
	fmt.Fprintln(blocks.Detail(), "step 1: uploading 10 files")
	fmt.Fprintln(blocks.Plain(), "Done.")
	fmt.Fprintln(&out)
	blocks.Detail().(interface{ Spaced() }).Spaced()
	fmt.Fprintln(blocks.Plain(), "Saved.")
	fmt.Fprintln(blocks.Detail(), "step 1: uploading 4 files")

	want := strings.Join([]string{
		"",
		"step 1: uploading 10 files",
		"",
		"step 1: uploading 10 files",
		"",
		"Done.",
		"",
		"Saved.",
		"",
		"step 1: uploading 4 files",
		"",
	}, "\n")
	if out.String() != want {
		t.Errorf("output =\n%q\nwant\n%q", out.String(), want)
	}
}

// Both writers unwrap to the destination, so term.IsTerminal sees through
// them.
func TestBlocksWritersUnwrap(t *testing.T) {
	var out bytes.Buffer
	blocks := NewBlocks(&out)
	if blocks.Detail().(detailWriter).Unwrap() != &out || blocks.Plain().(plainWriter).Unwrap() != &out {
		t.Error("writers do not unwrap to the destination")
	}
}
