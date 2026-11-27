package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"

	"github.com/spf13/cobra"

	"github.com/mittwald/cli-kit/notice"
	"github.com/mittwald/cli-kit/theme"
)

// newNoticeCommand returns the notice command: every theme in every set of
// colors, on a dark or a light background.
func newNoticeCommand() *cobra.Command {
	var themeName, colorsName, background string
	cmd := &cobra.Command{
		Use:   "notice",
		Short: "Show the notice themes and colors: error, warning, info, OK and debug lines",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runNotice(themeName, colorsName, background)
		},
	}
	cmd.Flags().StringVar(&themeName, "theme", "", "show only this theme: line, badge, bar or block")
	cmd.Flags().StringVar(&colorsName, "colors", "", "show only these colors: default, muted, deep or pastel")
	cmd.Flags().StringVar(&background, "background", "auto", "the terminal's background: auto, dark or light")
	return cmd
}

func runNotice(themeName, colorsName, background string) error {
	bg, ok := map[string]notice.Background{
		"auto": notice.BackgroundAuto, "dark": notice.BackgroundDark, "light": notice.BackgroundLight,
	}[background]
	if !ok {
		return fmt.Errorf("--background %q is not auto, dark or light", background)
	}
	names := theme.Schemes
	if colorsName != "" {
		s, ok := theme.ParseScheme(colorsName)
		if !ok {
			return fmt.Errorf("--colors %q is not one of %v", colorsName, names)
		}
		names = []theme.Scheme{s}
	}
	themes := notice.Themes
	if themeName != "" {
		i := slices.IndexFunc(themes, func(t notice.Theme) bool { return t.Name == themeName })
		if i < 0 {
			return fmt.Errorf("--theme %q is not line, badge, bar or block", themeName)
		}
		themes = themes[i : i+1]
	}

	// A tool's own role, picked out of messages by a pattern.
	mr := notice.NewRole("merge request", notice.RoleColors{Dark: theme.Fg256(218), Light: theme.RGB(0xA0, 0x30, 0x6E)})
	mrPattern := notice.Pattern{Match: regexp.MustCompile(`[\w./-]+![0-9]+`), Role: mr}

	title := theme.Bold.For(os.Stdout)
	for _, name := range names {
		for _, t := range themes {
			fmt.Println(title.Paint(fmt.Sprintf("── %s · %s ──", t.Name, name)))
			p := notice.New(notice.Options{
				Theme:      t.WithColors(notice.Schemes[name]).WithPatterns(mrPattern),
				Background: bg,
				Writer:     os.Stdout,
				Debug:      true,
			})
			p.Error(fmt.Errorf("could not read the replies on tools/nickpit!123: %w", errors.New("request timed out")))
			p.Warnf("Booking day %s was never left: the start at %s %s has no Gehen",
				notice.Date("19.11.2026"), notice.Date("19.11.2026"), notice.Time("08:40"))
			p.Infof("session %s saved to %s", notice.Ref("7f3a2c"), notice.Path("~/.local/state/nickpit/7f3a2c.json"))
			p.Successf("Booked Kommen at %s on %s", notice.Time("08:40"), notice.Date("25.11.2026"))
			p.Debugf("GET %s, %d retries left", notice.Path("https://gitlab.example/api/v4/projects/42"), notice.Detail(3))
			fmt.Println()
		}
	}
	return nil
}
