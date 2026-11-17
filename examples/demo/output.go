package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mittwald/cli-kit/cobrax"
	"github.com/mittwald/cli-kit/format"
	"github.com/mittwald/cli-kit/table"
	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// service is one row of the table command. Its json tags drive JSON and
// YAML alike.
type service struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Replicas int    `json:"replicas"`
	Image    string `json:"image"`
}

var services = []service{
	{"api", "running", 3, "registry.example.com/api:1.4.2"},
	{"worker", "stopped", 0, "registry.example.com/worker:1.4.2"},
	{"scheduler", "running", 1, "registry.example.com/scheduler:1.3.9"},
}

// newTableCommand returns the table command: the services in one style, in
// every style, or in the format --output names.
func newTableCommand() *cobra.Command {
	var (
		out       format.Format
		all       bool
		styleFlag *cobrax.Enum
	)
	cmd := &cobra.Command{
		Use:   "table",
		Short: "Draw a table in any style, or write it as JSON, YAML, CSV, TSV or Markdown",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if out.IsStructured() {
				return format.Write(os.Stdout, out, services)
			}
			if all && out.IsVerbatim() {
				return fmt.Errorf("--all draws the table and cannot be combined with --output %s", out)
			}
			if !all {
				s, err := table.ParseStyle(styleFlag.Value())
				if err != nil {
					return err
				}
				return renderServices(s, out)
			}
			for _, name := range table.StyleNames() {
				s, _ := table.LookupStyle(name)
				fmt.Println(theme.Active().For(os.Stdout).Title.Paint(name))
				if err := renderServices(s, out); err != nil {
					return err
				}
				fmt.Println()
			}
			return nil
		},
	}
	cmd.Flags().VarP(format.NewValue(&out, format.Text, nil), "output", "o",
		"output format: "+format.Default.String())
	styleFlag = cobrax.AddEnumFlag(cmd, cmd.Flags(), "style", "s", "rounded",
		"table style", table.StyleNames()...)
	cmd.Flags().BoolVar(&all, "all", false, "draw the table in every style")
	return cmd
}

// renderServices draws the services table in style s, or writes it in the
// record or Markdown format out selects.
func renderServices(s table.Style, out format.Format) error {
	t := table.New(os.Stdout, []string{"Name", "Status", "Replicas", "Image"},
		table.UppercaseHeaders(),
		table.WithStyle(s),
		table.WithAlignment(table.AlignLeft, table.AlignLeft, table.AlignRight),
		// Applied last: Markdown, CSV and TSV replace the style.
		table.WithFormat(out),
		table.EscapeFormulas(),
	)
	for _, svc := range services {
		t.Append(svc.Name, svc.Status, fmt.Sprint(svc.Replicas), svc.Image)
	}
	return t.Render()
}

const sampleDiff = "diff --git a/version.yaml b/version.yaml\n" +
	"--- a/version.yaml\n" +
	"+++ b/version.yaml\n" +
	"@@ -1,3 +1,3 @@\n" +
	" name: api\n" +
	"-version: 1.4.1\n" +
	"+version: 1.4.2\n" +
	" stable: true\n"

// newThemeCommand returns the theme command: what the color roles look like,
// and what the theme paints for diffs, JSON and names.
func newThemeCommand() *cobra.Command {
	var spec string
	cmd := &cobra.Command{
		Use:   "theme",
		Short: "Show the color roles, a painted diff, painted JSON and per-name colors",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runTheme(spec)
		},
	}
	cmd.Flags().StringVar(&spec, "style", "fg=white:bg=#aa0000:bold",
		`a style as a user would set it, such as "red:bold", "245" or "bg=#aa0000"`)
	return cmd
}

func runTheme(spec string) error {
	// Plain wherever stdout takes no color, so a pipe sees no escapes.
	th := theme.Active().For(os.Stdout)

	heading := func(s string) { fmt.Println(th.Title.Paint(s)) }
	heading("Roles")
	roles := []struct {
		name  string
		style theme.Style
	}{
		{"Info", th.Info}, {"Success", th.Success}, {"Warning", th.Warning}, {"Error", th.Error},
		{"Accent", th.Accent}, {"Flag", th.Flag}, {"Value", th.Value}, {"Qualifier", th.Qualifier},
		{"Muted", th.Muted}, {"Added", th.Added}, {"Removed", th.Removed},
	}
	for _, r := range roles {
		fmt.Println("  " + r.style.Paint(r.name))
	}

	fmt.Println()
	heading("Diff")
	fmt.Print(th.PaintDiff(sampleDiff))

	fmt.Println()
	heading("JSON")
	fmt.Println(th.PaintJSON(`{"name":"api","replicas":3,"stable":true,"owner":null}`))

	fmt.Println()
	heading("A stable color per name")
	// A nil palette means theme.NamePalette. Case and surrounding space do
	// not change the color, so both spellings of alice match.
	for _, name := range []string{"alice", "bob", "carol", "Alice"} {
		fmt.Println("  " + th.Hashed(name).Paint(name))
	}

	fmt.Println()
	heading("Style from --style")
	style, ok := theme.ParseStyle(spec)
	if !ok {
		return fmt.Errorf("--style %q is no style", spec)
	}
	fmt.Println("  " + style.For(os.Stdout).Paint(spec))
	return nil
}

// newTermCommand returns the term command: what term does to text before it
// reaches the terminal.
func newTermCommand(links *cobrax.ModeValue) *cobra.Command {
	return &cobra.Command{
		Use:   "term",
		Short: "Show hyperlinks, truncation, wrapping and hostile text made safe",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			runTerm(links.Mode())
			return nil
		},
	}
}

func runTerm(linkMode term.ColorMode) {
	th := theme.Active().For(os.Stdout)
	width := term.Columns(os.Stdout, 80)
	heading := func(s string) { fmt.Println(th.Title.Paint(s)) }

	heading("Hyperlinks")
	const docs = "https://pkg.go.dev/github.com/mittwald/cli-kit"
	if term.ShouldLink(linkMode, os.Stdout) {
		fmt.Println("  " + term.Hyperlink(docs, "the package documentation"))
	} else {
		fmt.Println("  the package documentation: " + docs)
	}

	fmt.Println()
	heading(fmt.Sprintf("Truncation to %d columns", width/2))
	long := "registry.example.com/team/very-long-service-name/with/a/deep/path:1.4.2-rc.1+build.20261117"
	fmt.Println("  end:    " + term.Truncate(long, width/2))
	fmt.Println("  middle: " + term.TruncateMiddle(long, width/2))
	fmt.Println("  wide:   " + term.Truncate("日本語のサービス名はとても長いです", 20))

	fmt.Println()
	heading("Wrapping")
	fmt.Println(term.Wrap("  Dry run: would tag 1.4.2, push it, wait for its pipeline and deploy it to every region one after the other.",
		min(width, 60), "           "))

	fmt.Println()
	heading("Hostile text")
	hostile := "\x1b]0;pwned\x07\x1b[31mURGENT\x1b[0m: invoice\u202efdp.exe\x1b[2J"
	fmt.Printf("  raw:       %q\n", hostile)
	fmt.Printf("  Sanitize:  %s\n", term.Sanitize(hostile))
	fmt.Printf("  PlainLine: %s\n", term.PlainLine(hostile))
	fmt.Printf("  width of %q: %d columns\n", "日本 ok", term.VisibleWidth("日本 ok"))
}
