package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/mittwald/cli-kit/theme"
)

func TestSolid(t *testing.T) {
	fg, bg, _ := colors(Solid(theme.Fg256(221)))
	if bg.c != palette256(221) || luminance(fg.c) > 0.1 {
		t.Errorf("Solid(yellow) = %q", Solid(theme.Fg256(221)))
	}
	fg, bg, _ = colors(Solid(theme.RGB(75, 56, 232)))
	if bg.c != (rgb{75, 56, 232}) || fg.c != palette256(255) {
		t.Errorf("Solid(blue) = %q", Solid(theme.RGB(75, 56, 232)))
	}
	if got := Solid("31"); !strings.Contains(string(got), "7") {
		t.Errorf("Solid of the terminal's red = %q, want reverse video", got)
	}
}

func TestGradientText(t *testing.T) {
	l := GradientText("NEXUS", MittwaldGradient, 0, theme.Bold)
	if l.String() != "NEXUS" || len(l) != 5 {
		t.Fatalf("GradientText = %#v", l)
	}
	if l[0].Style == l[4].Style {
		t.Errorf("one color throughout: %q", l[0].Style)
	}
	if moved := GradientText("NEXUS", MittwaldGradient, 0.3, theme.Bold); moved[0].Style == l[0].Style {
		t.Errorf("phase did not move the colors")
	}
	if plain := GradientText("x", nil, 0, ""); len(plain) != 1 || plain[0].Style != "" {
		t.Errorf("no stops = %#v", plain)
	}
}

func TestShimmerAnimates(t *testing.T) {
	s := NewShimmer("NEXUS")
	a := New(VBox(Fit(s)))
	if !a.moving() {
		t.Error("an active shimmer does not redraw")
	}
	s.SetActive(false)
	if a.moving() {
		t.Error("a still shimmer redraws")
	}
}

func TestProgressBar(t *testing.T) {
	p := NewProgressBar("loading", PastelColors[0])
	p.glide = 0
	h := NewHeadless(New(VBox(Fit(p))), 20, 1)
	p.SetValue(0.5)
	h.Draw()
	if got := h.Lines()[0]; !strings.HasPrefix(got, " loading") || !strings.HasSuffix(strings.TrimRight(got, " "), "50%") {
		t.Errorf("bar = %q", got)
	}
	filled, track := h.buf.at(0, 0).style, h.buf.at(19, 0).style
	if _, bg, _ := colors(filled); bg.c != (rgb{177, 185, 249}) {
		t.Errorf("fill = %q", filled)
	}
	if _, bg, _ := colors(track); bg.c == (rgb{177, 185, 249}) {
		t.Errorf("track = %q", track)
	}
	p.glide = time.Hour
	p.SetValue(1)
	if !p.Animating() {
		t.Error("a gliding bar does not animate")
	}
}

func TestPanelTitleBar(t *testing.T) {
	l := NewList(func(s string) []Span { return []Span{{Text: s}} })
	l.SetItems([]string{"a"})
	p := NewPanel("Tickets", l)
	p.SetFooter(Line{{Text: "1/1"}})
	h := NewHeadless(New(p), 20, 3)
	if got := h.Lines()[0]; !strings.HasPrefix(got, " Tickets ") || !strings.HasSuffix(strings.TrimRight(got, " "), "1/1") {
		t.Errorf("title bar = %q", got)
	}
	if strings.ContainsAny(h.Screen(), "╭│╰") {
		t.Errorf("an outline:\n%s", h.Screen())
	}
	if h.buf.at(1, 0).style != theme.Join(h.app.theme.Bar, h.app.theme.Badge) {
		t.Errorf("badge = %q", h.buf.at(1, 0).style)
	}
}

func TestStatusBarNotice(t *testing.T) {
	a := New(VBox(Grow(Spacer{}), Fit(NewStatusBar(func() Line { return Text("left", "") }, nil))))
	h := NewHeadless(a, 30, 2)
	a.Notify(LevelWarning, "Booking day never left")
	h.Draw()
	if got := h.Lines()[1]; !strings.HasPrefix(got, " WARN  Booking day") {
		t.Errorf("notice = %q", got)
	}
	_, bg, _ := colors(h.buf.at(29, 1).style)
	if _, want, _ := colors(Solid(a.theme.Warning)); bg != want {
		t.Errorf("the bar is not filled with the warning: %q", h.buf.at(29, 1).style)
	}
}

func TestThemeOf(t *testing.T) {
	t.Setenv("COLORFGBG", "0;15")
	if ThemeOf(theme.SchemeMuted) != Schemes[theme.SchemeMuted].Light {
		t.Error("a light terminal got the dark theme")
	}
	t.Setenv("COLORFGBG", "15;0")
	if ThemeOf("nope") != Schemes[theme.SchemeDefault].Dark {
		t.Error("an unknown scheme is not the default")
	}
	for _, s := range theme.Schemes {
		if _, ok := Schemes[s]; !ok {
			t.Errorf("no theme for scheme %q", s)
		}
	}
}

func TestTo256(t *testing.T) {
	saved := trueColor
	t.Cleanup(func() { trueColor = saved })
	trueColor = func() bool { return false }
	if got := attributes("1;38;2;75;56;232;48;2;255;255;255", true); got != "1;38;5;62;48;5;231" {
		t.Errorf("attributes = %q", got)
	}
	trueColor = func() bool { return true }
	if got := attributes("38;2;75;56;232", true); got != "38;2;75;56;232" {
		t.Errorf("attributes = %q", got)
	}
}
