package notice

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// colored makes every writer take color, in 24-bit colors on a dark
// background, for the rest of the test.
func colored(t *testing.T) {
	t.Helper()
	was := term.CurrentColorMode()
	t.Cleanup(func() { term.SetColorMode(was) })
	term.SetColorMode(term.ColorAlways)
	t.Setenv("COLORTERM", "truecolor")
	t.Setenv("COLORFGBG", "")
}

func printer(buf *bytes.Buffer, options Options) *Printer {
	options.Writer = buf
	return New(options)
}

func TestPlainOutput(t *testing.T) {
	var buf bytes.Buffer
	p := printer(&buf, Options{Theme: Badge})
	p.Errorf("failed: %s", Detail("timeout"))
	p.Warnf("day %s", Date("19.11.2026"))
	p.Infof("info")
	p.Successf("done")
	p.Debugf("hidden")
	want := "ERROR: failed: timeout\nWARN: day 19.11.2026\nINFO: info\nOK: done\n"
	if got := buf.String(); got != want {
		t.Fatalf("plain output = %q, want %q", got, want)
	}
}

func TestDebug(t *testing.T) {
	var buf bytes.Buffer
	printer(&buf, Options{Debug: true}).Debugf("GET %s", Path("/x"))
	printer(&buf, Options{Theme: Block, Debug: true}).Debugf("GET %s", Path("/x"))
	if got, want := buf.String(), "+ GET /x\nDEBUG: GET /x\n"; got != want {
		t.Fatalf("debug = %q, want %q", got, want)
	}
}

func TestErrorAndWarnTakeErrors(t *testing.T) {
	var buf bytes.Buffer
	p := printer(&buf, Options{})
	p.Error(nil)
	p.Warn(nil)
	p.Error(errors.New("boom"))
	p.Warn(errors.New("careful"))
	if got, want := buf.String(), "ERROR: boom\nWARN: careful\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestCleansUntrustedText(t *testing.T) {
	var buf bytes.Buffer
	printer(&buf, Options{}).Warnf("server said %s, value %s", "\x1b[2Jpassword=hunter2", Detail("\x1b]0;title\x07token=abc"))
	got := buf.String()
	if strings.Contains(got, "\x1b") || strings.Contains(got, "hunter2") || strings.Contains(got, "\x07") {
		t.Fatalf("untrusted text not cleaned: %q", got)
	}

	buf.Reset()
	printer(&buf, Options{KeepEscapes: true, NoRedact: true}).Warnf("%s", "\x1b[1mpassword=hunter2")
	if got := buf.String(); !strings.Contains(got, "\x1b[1m") || !strings.Contains(got, "hunter2") {
		t.Fatalf("KeepEscapes/NoRedact still cleaned: %q", got)
	}
}

func TestMarksCannotBeForged(t *testing.T) {
	var buf bytes.Buffer
	forged := string([]rune{markOpen}) + "0000000000000000" + "0" + string([]rune{markClose})
	printer(&buf, Options{}).Infof("%s and %s", forged, Ref("real"))
	if got, want := buf.String(), "INFO: "+forged+" and real\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestRedactsAcrossValues(t *testing.T) {
	var buf bytes.Buffer
	printer(&buf, Options{}).Infof("Authorization: Bearer %s", Detail("abcdefgh12345678"))
	printer(&buf, Options{}).Infof("password=%s", Value(RoleRef, "hunter2"))
	if got := buf.String(); strings.Contains(got, "abcdefgh12345678") || strings.Contains(got, "hunter2") {
		t.Fatalf("credential in a value not redacted: %q", got)
	}
}

func TestOrdinaryArgumentsKeepFmtSemantics(t *testing.T) {
	var buf bytes.Buffer
	printer(&buf, Options{}).Infof("[%*s] %T %v", 5, "x", 42, Detail("d"))
	if got, want := buf.String(), "INFO: [    x] int d\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestTabsCountValues(t *testing.T) {
	var plain, valued bytes.Buffer
	printer(&plain, Options{}).Infof("%s\tend", "x")
	printer(&valued, Options{}).Infof("%s\tend", Detail("x"))
	if plain.String() != valued.String() || plain.String() != "INFO: x       end\n" {
		t.Fatalf("tab expanded differently: %q vs %q", plain.String(), valued.String())
	}
}

func TestCustomThemeKeepsItsSettings(t *testing.T) {
	colored(t)
	var buf bytes.Buffer
	printer(&buf, Options{Theme: Theme{Layout: LayoutBadge, Padding: Padding{2, 2}}}).Infof("x")
	if got := buf.String(); !strings.Contains(got, "m  INFO   \x1b[0m") {
		t.Fatalf("custom theme replaced: %q", got)
	}
}

func TestValueVerbs(t *testing.T) {
	var buf bytes.Buffer
	printer(&buf, Options{}).Infof("%d retries, %5.1f%%, %q", Detail(3), Detail(42.0), Ref("x"))
	if got, want := buf.String(), "INFO: 3 retries,  42.0%, \"x\"\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestMultiLine(t *testing.T) {
	var buf bytes.Buffer
	printer(&buf, Options{}).Errorf("first\nsecond")
	if got, want := buf.String(), "ERROR: first\n       second\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestAround(t *testing.T) {
	var buf bytes.Buffer
	calls := 0
	p := printer(&buf, Options{Around: func(write func()) { calls++; write() }})
	p.Infof("x")
	if calls != 1 || buf.String() != "INFO: x\n" {
		t.Fatalf("Around called %d times, output %q", calls, buf.String())
	}
}

func TestLineColors(t *testing.T) {
	colored(t)
	var buf bytes.Buffer
	printer(&buf, Options{Theme: Line}).Warnf("day %s at %s", Date("19.11.2026"), Time("08:40"))
	got := buf.String()
	want := theme.Fg256(221).Paint("WARN") + theme.Fg256(244).Paint(":") + " day " +
		theme.RGB(0x5F, 0xAF, 0xD7).Paint("19.11.2026") + " at " + theme.RGB(0xEC, 0xB4, 0x4A).Paint("08:40") + "\n"
	if got != want {
		t.Fatalf("line =\n%q\nwant\n%q", got, want)
	}
}

func TestBadgePaddingAndFill(t *testing.T) {
	colored(t)
	var buf bytes.Buffer
	printer(&buf, Options{Theme: Badge.WithPadding(0, 2)}).Infof("x")
	if got := buf.String(); !strings.Contains(got, "mINFO   \x1b[0m x") {
		t.Fatalf("badge not padded 0/2: %q", got)
	}

	buf.Reset()
	printer(&buf, Options{Theme: Block}).Infof("x")
	if !strings.Contains(buf.String(), "\x1b[K") {
		t.Fatalf("block not filled: %q", buf.String())
	}
	buf.Reset()
	printer(&buf, Options{Theme: Bar.WithoutFill()}).Infof("x")
	if strings.Contains(buf.String(), "\x1b[K") {
		t.Fatalf("bar filled although WithoutFill: %q", buf.String())
	}
}

func TestBlockValuesAreBoldOnAccent(t *testing.T) {
	colored(t)
	var buf bytes.Buffer
	printer(&buf, Options{Theme: Block}).Warnf("day %s", Date("19.11.2026"))
	want := theme.Join(theme.Bold, theme.RGB(0x14, 0x32, 0x6E)).Sequence() + "19.11.2026" + theme.Reset +
		theme.Join(theme.BgRGB(0xEC, 0xB4, 0x4A), "30").Sequence()
	if got := buf.String(); !strings.Contains(got, want) {
		t.Fatalf("block value not bold on accent:\n%q\nwant within\n%q", got, want)
	}
}

func TestLightBackground(t *testing.T) {
	colored(t)
	var buf bytes.Buffer
	printer(&buf, Options{Background: BackgroundLight}).Warnf("x")
	if got := buf.String(); !strings.HasPrefix(got, theme.RGB(0xA6, 0x6B, 0x00).Sequence()+"WARN") {
		t.Fatalf("light label not used: %q", got)
	}

	buf.Reset()
	t.Setenv("COLORFGBG", "0;15")
	printer(&buf, Options{}).Warnf("x")
	if got := buf.String(); !strings.HasPrefix(got, theme.RGB(0xA6, 0x6B, 0x00).Sequence()) {
		t.Fatalf("COLORFGBG light background not detected: %q", got)
	}
}

func TestColorsOverride(t *testing.T) {
	colored(t)
	var buf bytes.Buffer
	printer(&buf, Options{Colors: Colors{
		Dark: Palette{
			Levels: map[Level]LevelColors{LevelWarn: {Label: theme.Fg256(1)}},
			Values: map[Role]theme.Style{RoleDate: theme.Fg256(2)},
		},
	}}).Warnf("%s %s", Date("d"), Time("t"))
	got := buf.String()
	if !strings.HasPrefix(got, theme.Fg256(1).Paint("WARN")) || !strings.Contains(got, theme.Fg256(2).Paint("d")) ||
		!strings.Contains(got, theme.RGB(0xEC, 0xB4, 0x4A).Paint("t")) {
		t.Fatalf("override not merged: %q", got)
	}
}

func TestCustomRoleAndPattern(t *testing.T) {
	colored(t)
	mr := NewRole("mr", RoleColors{Dark: theme.Fg256(218), Light: theme.Fg256(125), OnAccent: map[Level]theme.Style{LevelInfo: theme.Fg256(53)}})
	th := Line.WithPatterns(Pattern{Match: regexp.MustCompile(`\w+![0-9]+`), Role: mr})

	var buf bytes.Buffer
	printer(&buf, Options{Theme: th}).Infof("see nickpit!12 and %s", Ref("nickpit!13"))
	got := buf.String()
	if !strings.Contains(got, theme.Fg256(218).Paint("nickpit!12")) {
		t.Fatalf("pattern not colored: %q", got)
	}
	if !strings.Contains(got, theme.Fg256(218).Paint("nickpit!13")) {
		t.Fatalf("value colored by pattern or lost: %q", got)
	}

	buf.Reset()
	printer(&buf, Options{Theme: th, Background: BackgroundLight}).Infof("nickpit!12")
	if !strings.Contains(buf.String(), theme.Fg256(125).Paint("nickpit!12")) {
		t.Fatalf("light role color not used: %q", buf.String())
	}

	buf.Reset()
	printer(&buf, Options{Theme: Block.WithPatterns(th.Patterns...)}).Infof("nickpit!12")
	if !strings.Contains(buf.String(), theme.Join(theme.Bold, theme.Fg256(53)).Sequence()+"nickpit!12") {
		t.Fatalf("OnAccent role color not used: %q", buf.String())
	}
	buf.Reset()
	printer(&buf, Options{Theme: Block.WithPatterns(th.Patterns...)}).Warnf("nickpit!12")
	if !strings.Contains(buf.String(), theme.Bold.Sequence()+"nickpit!12") {
		t.Fatalf("role without OnAccent not bold in the text color: %q", buf.String())
	}
}

func TestNewRoleNeedsBothColors(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewRole without a Light color did not panic")
		}
	}()
	NewRole("x", RoleColors{Dark: theme.Fg256(1)})
}

func TestWithMethodsCopy(t *testing.T) {
	changed := Line.WithPadding(3, 3).WithoutFill().WithPatterns(Pattern{Role: RoleRef})
	if Line.Padding != (Padding{1, 1}) || Line.NoFill || len(Line.Patterns) != 0 {
		t.Fatalf("built-in Line changed: %+v", Line)
	}
	if changed.Padding != (Padding{3, 3}) || !changed.NoFill || len(changed.Patterns) != 1 {
		t.Fatalf("copy not changed: %+v", changed)
	}
}

func TestAdaptTo256(t *testing.T) {
	if got := adapt(theme.Join(theme.Bold, theme.BgRGB(0xEC, 0xB4, 0x4A), theme.RGB(0, 0, 0)), false); got != "1;48;5;215;38;5;16" {
		t.Fatalf("adapt = %q", got)
	}
	if got := adapt(theme.RGB(1, 2, 3), true); got != theme.RGB(1, 2, 3) {
		t.Fatalf("adapt changed a style on a 24-bit terminal: %q", got)
	}
}

func TestBuiltinPalettesAreComplete(t *testing.T) {
	for name, c := range Schemes {
		for _, level := range Levels {
			lc := c.Dark.Levels[level]
			if lc.Label == "" || lc.Accent == "" || lc.Text == "" || lc.Tint == "" || lc.TintText == "" {
				t.Errorf("%s: dark %s incomplete: %+v", name, level, lc)
			}
			for _, role := range Roles {
				if lc.Values[role] == "" {
					t.Errorf("%s: dark %s has no block color for %s", name, level, role.Name())
				}
			}
			light := c.Light.Levels[level]
			if light.Label == "" || light.Tint == "" || light.TintText == "" {
				t.Errorf("%s: light %s incomplete: %+v", name, level, light)
			}
		}
		for _, role := range Roles {
			if c.Dark.Values[role] == "" || c.Light.Values[role] == "" {
				t.Errorf("%s: no terminal color for %s", name, role.Name())
			}
		}
	}
}
