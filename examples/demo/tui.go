package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mittwald/cli-kit/theme"
	"github.com/mittwald/cli-kit/tui"
)

func newTUICommand() *cobra.Command {
	var mouse bool
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "A ticket tool built from package tui, on made-up tickets (needs a terminal)",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			app := newTicketApp(mouse)
			err := app.Run()
			if errors.Is(err, tui.ErrNotATerminal) {
				fmt.Fprintln(os.Stderr, "demo: tui needs a terminal")
				return nil
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&mouse, "mouse", false, "take clicks and the wheel")
	return cmd
}

// ticket is a made-up ticket.
type ticket struct {
	id         string
	title      string
	customer   string
	status     string
	department string
	high       bool
	labels     []string
	assignee   string
	subscribed bool
	resubmit   string
	updated    time.Time
	messages   []message
}

type message struct {
	author   string
	at       time.Time
	internal bool
	text     string
}

var (
	statuses    = []string{"open", "inProgress", "waiting", "answered", "closed"}
	departments = []string{"KS", "SWT", "CH", "DEV"}
	allLabels   = []string{"Apps", "Backup", "Datenbank", "DNS", "Domains", "Mail", "Performance", "SSL", "Umzug", "mStudio"}
)

func sampleTickets() []*ticket {
	now := time.Now()
	titles := []string{
		"Serverperformance",
		"Anfrage auf Änderung des Versandlimits: seminarverwaltung@example.com",
		"Anfrage auf Änderung des Versandlimits: rechnung@example.com",
		"Umzug von https://www.example.org/",
		"mStudio sitedraft AI - Zugriff nicht mehr vorhanden",
		"Zertifikat läuft ab: shop.example.net",
		"Backup wiederherstellen für p-ab12cd",
		"DNS-Einträge für neue Domain",
		"Cronjob läuft nicht mehr seit Update",
		"Datenbank-Import bricht ab (max_allowed_packet)",
		"Kündigung Projekt p-zz99yy",
		"Mails landen im Spam 📬",
	}
	people := []string{"Thomas N.", "Arne H.", "Bianca B.", "Karl D.", "Olga E.", "Tim N."}
	var out []*ticket
	for i := range 40 {
		t := &ticket{
			id:         fmt.Sprintf("m-%07d", 3290102+i*7919%999983),
			title:      titles[i%len(titles)],
			customer:   people[i%len(people)],
			status:     statuses[i%len(statuses)],
			department: departments[i%len(departments)],
			high:       i%3 == 0,
			labels:     []string{allLabels[i%len(allLabels)], allLabels[(i*3+1)%len(allLabels)]},
			subscribed: i%2 == 0,
			updated:    now.Add(-time.Duration(i*37) * time.Minute),
		}
		for j := range 3 + i%5 {
			t.messages = append(t.messages, message{
				author:   []string{t.customer, "Support"}[j%2],
				at:       t.updated.Add(-time.Duration(5-j) * time.Hour),
				internal: j%4 == 3,
				text: fmt.Sprintf("Hallo,\n\ndies ist Nachricht %d zum Ticket „%s“. Sie ist lang genug, um im Fenster umzubrechen, "+
					"damit man sieht, wie die Ansicht scrollt und sucht.\n\nViele Grüße", j+1, t.title),
			})
		}
		out = append(out, t)
	}
	return out
}

// ticketApp is the state of the demo.
type ticketApp struct {
	app     *tui.App
	pages   *tui.Pages
	tickets []*ticket
	list    *tui.List[*ticket]
	panel   *tui.Panel
	filters struct {
		statuses   map[string]bool
		department string
		mine       bool
		high       bool
	}

	// The detail page.
	current  *ticket
	thread   *tui.Viewport
	threadP  *tui.Panel
	reply    *tui.TextArea
	replyP   *tui.Panel
	internal *tui.Toggle
	details  *tui.List[detailRow]
}

// detailRow is a line of the details pane, with what Enter does on it.
type detailRow struct {
	label string
	value func() tui.Line
	edit  func()
}

func newTicketApp(mouse bool) *tui.App {
	t := &ticketApp{tickets: sampleTickets()}
	t.filters.statuses = map[string]bool{"open": true, "inProgress": true, "waiting": true}
	opts := []tui.Option{}
	if mouse {
		opts = append(opts, tui.WithMouse())
	}
	t.pages = tui.NewPages()
	t.pages.Add("list", t.listPage())
	t.pages.Add("ticket", t.ticketPage())
	status := tui.NewStatusBar(
		func() tui.Line {
			return tui.Line{{Text: "demo · tickets", Style: theme.Bold}}
		},
		func() tui.Line {
			if t.pages.Current() == "ticket" {
				return tui.Text(t.thread.Position()+" ", "")
			}
			return tui.Text(t.list.Position()+" ", "")
		})
	root := tui.VBox(tui.Grow(t.pages), tui.Fit(tui.NewLegend()), tui.Fit(status))
	t.app = tui.New(root, opts...)
	km := t.app.Keymap()
	km.Bind("quit", t.quit, "q").InGroup("General")
	km.Bind("suspend", t.app.Suspend, "ctrl+z").Hide().InGroup("General")
	t.refresh()
	return t.app
}

func (t *ticketApp) quit() {
	if t.pages.Current() == "ticket" {
		t.back()
		return
	}
	t.app.Quit()
}

var statusStyles = map[string]theme.Style{
	"open":       theme.Join(theme.Bold, theme.Fg256(203)),
	"inProgress": theme.Fg256(141),
	"waiting":    theme.Fg256(214),
	"answered":   theme.Fg256(244),
	"closed":     theme.Fg256(244),
}

func age(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func (t *ticketApp) listPage() tui.Widget {
	t.list = tui.NewList(func(k *ticket) []tui.Span {
		prio := ""
		if k.high {
			prio = "!"
		}
		return []tui.Span{
			{Text: k.id},
			{Text: prio, Style: theme.Join(theme.Bold, theme.Fg256(203))},
			{Text: k.status, Style: statusStyles[k.status]},
			{Text: k.department, Style: theme.Fg256(117)},
			{Text: k.title},
			{Text: strings.Join(k.labels, ", "), Style: theme.Fg256(244)},
			{Text: k.customer},
			{Text: age(k.updated), Style: theme.Fg256(244)},
		}
	}).SetColumns(
		tui.Column{Title: "ID"},
		tui.Column{Title: "P", Width: 1},
		tui.Column{Title: "STATUS"},
		tui.Column{Title: "DEPT"},
		tui.Column{Title: "TITLE", Grow: 1, Min: 12},
		tui.Column{Title: "LABELS", Min: 6},
		tui.Column{Title: "CUSTOMER", Min: 6},
		tui.Column{Title: "AGE", Align: tui.AlignRight},
	).SetEmpty("No tickets match the filters").
		SetRowStyle(func(k *ticket) theme.Style {
			if k.status == "closed" {
				return theme.Dim
			}
			return ""
		})
	t.list.OnSelect = t.open
	km := t.list.Keymap()
	km.Bind("filters", t.filterDialog, "f").InGroup("Tickets")
	km.Bind("actions", t.rowMenu, "m", ".").InGroup("Tickets")
	km.Bind("assign", func() { t.assign(t.list.Marked()...) }, "a").InGroup("Tickets")
	km.Bind("labels", func() { t.labelDialog(t.list.Marked()...) }, "l").InGroup("Tickets")
	km.Bind("priority", func() { t.togglePrio(t.list.Marked()...) }, "p").InGroup("Tickets")
	km.Bind("refresh", t.reload, "r", "ctrl+r").InGroup("Tickets")
	t.panel = tui.NewPanel("Tickets", t.list)
	return t.panel
}

// refresh applies the filters to the tickets.
func (t *ticketApp) refresh() {
	var shown []*ticket
	for _, k := range t.tickets {
		if len(t.filters.statuses) > 0 && !t.filters.statuses[k.status] {
			continue
		}
		if t.filters.department != "" && k.department != t.filters.department {
			continue
		}
		if t.filters.mine && k.assignee != "me" {
			continue
		}
		if t.filters.high && !k.high {
			continue
		}
		shown = append(shown, k)
	}
	slices.SortStableFunc(shown, func(a, b *ticket) int { return b.updated.Compare(a.updated) })
	t.list.SetItems(shown)
	var parts []string
	for _, s := range statuses {
		if t.filters.statuses[s] {
			parts = append(parts, s)
		}
	}
	title := tui.Line{{Text: "Tickets"}}
	if len(parts) > 0 {
		title = append(title, tui.Span{Text: " · " + strings.Join(parts, ","), Style: theme.Fg256(244)})
	}
	if t.filters.department != "" {
		title = append(title, tui.Badge(t.filters.department, theme.Join(theme.Reverse, theme.Fg256(117))))
	}
	if t.filters.mine {
		title = append(title, tui.Span{Text: " · mine", Style: theme.Fg256(244)})
	}
	t.panel.SetTitleLine(title)
}

// reload pretends to fetch the tickets again.
func (t *ticketApp) reload() {
	done := t.app.Busy("loading tickets…")
	t.list.SetLoading(true)
	t.app.Go(func() func() {
		time.Sleep(800 * time.Millisecond)
		return func() {
			done()
			t.list.SetLoading(false)
			t.refresh()
			t.app.Notify(tui.LevelSuccess, "Tickets loaded")
		}
	})
}

func (t *ticketApp) filterDialog() {
	status := tui.NewCheckList(statuses).SetHorizontal(true)
	for i, s := range statuses {
		status.SetChecked(i, t.filters.statuses[s])
	}
	dept := tui.NewRadioGroup(append([]string{"any"}, departments...), 0).SetHorizontal(true)
	if i := slices.Index(departments, t.filters.department); i >= 0 {
		dept.SetSelected(i + 1)
	}
	mine := tui.NewSwitch("only assigned to me", t.filters.mine)
	high := tui.NewSwitch("only high priority", t.filters.high)
	form := tui.NewForm().
		Add("Status", status).
		Add("Department", dept).
		Add("Assigned", mine).
		Add("Priority", high)
	var d *tui.Dialog
	apply := func() {
		t.filters.statuses = map[string]bool{}
		for _, v := range status.Values() {
			t.filters.statuses[v] = true
		}
		t.filters.department = ""
		if dept.Selected() > 0 {
			t.filters.department = dept.Value()
		}
		t.filters.mine, t.filters.high = mine.Value(), high.Value()
		t.app.Close(d)
		t.refresh()
	}
	form.OnSubmit = apply
	reset := func() {
		t.filters.statuses = map[string]bool{}
		t.filters.department, t.filters.mine, t.filters.high = "", false, false
		t.app.Close(d)
		t.refresh()
	}
	d = tui.NewDialog("Filters", form, tui.NewButton("Apply", apply), tui.NewButton("Reset", reset)).
		SetFooter("tab next · space toggle · ctrl+s apply · esc cancel")
	t.app.Push(d, tui.LayerOptions{})
}

func (t *ticketApp) rowMenu() {
	k, ok := t.list.Selected()
	if !ok {
		return
	}
	t.app.Menu(k.id, []tui.MenuItem{
		{Key: "enter", Label: "Open", Action: func() { t.open(k) }},
		{Key: "a", Label: "Assign to me", Action: func() { t.assign(k) }},
		{Key: "p", Label: "Toggle priority", Action: func() { t.togglePrio(k) }},
		{Key: "l", Label: "Labels…", Action: func() { t.labelDialog(k) }},
		{Key: "s", Label: "Status…", Action: func() { t.statusDialog(k) }},
		{Key: "w", Label: "Resubmission…", Action: func() { t.resubmitDialog(k) }},
		{Key: "r", Label: "Rename…", Action: func() { t.rename(k) }},
		{Separator: true},
		{Key: "c", Label: "Close ticket", Action: func() { t.closeTicket(k) }, Disabled: k.status == "closed"},
	})
}

func (t *ticketApp) assign(ks ...*ticket) {
	for _, k := range ks {
		k.assignee = "me"
		if k.status == "open" {
			k.status = "inProgress"
		}
	}
	t.list.ClearMarks()
	t.refresh()
	t.app.Notifyf(tui.LevelSuccess, "Assigned %d ticket(s) to you", len(ks))
}

func (t *ticketApp) togglePrio(ks ...*ticket) {
	for _, k := range ks {
		k.high = !k.high
	}
	t.list.ClearMarks()
	t.refresh()
}

func (t *ticketApp) labelDialog(ks ...*ticket) {
	if len(ks) == 0 {
		return
	}
	cl := tui.NewCheckList(allLabels)
	for i, l := range allLabels {
		cl.SetChecked(i, slices.Contains(ks[0].labels, l))
	}
	var d *tui.Dialog
	save := func() {
		for _, k := range ks {
			k.labels = cl.Values()
		}
		t.app.Close(d)
		t.list.ClearMarks()
		t.app.Notifyf(tui.LevelSuccess, "Labels of %d ticket(s) saved", len(ks))
	}
	form := tui.NewForm().Add("", cl)
	form.OnSubmit = save
	d = tui.NewDialog(fmt.Sprintf("Labels (%d tickets)", len(ks)), form, tui.NewButton("Save", save)).
		SetFooter("space check · a all · n none · ctrl+s save")
	t.app.Push(d, tui.LayerOptions{Focus: cl})
}

func (t *ticketApp) statusDialog(k *ticket) {
	t.app.Choose("Status of "+k.id, statuses, slices.Index(statuses, k.status), func(i int) {
		k.status = statuses[i]
		t.refresh()
		t.app.Notifyf(tui.LevelInfo, "%s is %s now", k.id, k.status)
	})
}

func (t *ticketApp) resubmitDialog(k *ticket) {
	presets := []string{"in 24 hours", "in 48 hours", "tomorrow 8:00", "in a week", "none"}
	t.app.Choose("Resubmission of "+k.id, presets, -1, func(i int) {
		k.resubmit = presets[i]
		if presets[i] == "none" {
			k.resubmit = ""
		}
	})
}

func (t *ticketApp) rename(k *ticket) {
	in := t.app.Prompt("Rename "+k.id, "Title", k.title, func(s string) {
		k.title = s
		t.app.Notify(tui.LevelSuccess, "Renamed")
	})
	in.Validate = func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New("the title cannot be empty")
		}
		return nil
	}
}

func (t *ticketApp) closeTicket(k *ticket) {
	t.app.Confirm("Close "+k.id, "Close “"+k.title+"”? The customer is notified.", func() {
		k.status = "closed"
		t.refresh()
		t.app.Notifyf(tui.LevelSuccess, "%s closed", k.id)
	})
}

// The ticket page.

func (t *ticketApp) ticketPage() tui.Widget {
	t.thread = tui.NewViewport()
	t.threadP = tui.NewPanel("Conversation", t.thread)
	t.reply = tui.NewTextArea().SetLimit(8000).SetPlaceholder("Write a reply… (Markdown)")
	t.internal = tui.NewSwitch("internal", true)
	t.replyP = tui.NewPanel("Reply", tui.VBox(tui.Grow(t.reply), tui.Fit(t.internal)))
	t.reply.OnChange = func(string) { t.replyFooter() }
	t.internal.OnChange = func(bool) { t.replyFooter() }
	t.details = tui.NewList(func(r detailRow) []tui.Span {
		return append([]tui.Span{{Text: fmt.Sprintf("%-11s", r.label), Style: theme.Fg256(244)}}, r.value()...)
	})
	t.details.OnSelect = func(r detailRow) {
		if r.edit != nil {
			r.edit()
		}
	}
	t.details.Keymap().Unbind("/")
	t.details.Keymap().Unbind("space")
	left := tui.VBox(tui.GrowBy(3, t.threadP), tui.GrowBy(2, t.replyP))
	right := tui.NewPanel("Details", t.details)
	page := tui.NewKeyed(tui.HBox(tui.GrowBy(3, left), tui.GrowBy(1, right)))
	km := page.Keymap()
	km.Bind("back", t.back, "esc").InGroup("Ticket")
	km.Bind("send", t.send, "ctrl+s").InGroup("Ticket")
	km.Bind("internal", t.internal.Flip, "ctrl+t").InGroup("Ticket")
	km.Bind("reply", func() { t.app.Focus(t.reply) }, "r").InGroup("Ticket")
	km.Bind("assign", func() { t.assign(t.current); t.fillDetails() }, "a").InGroup("Ticket")
	km.Bind("labels", func() { t.labelDialog(t.current) }, "l").InGroup("Ticket")
	km.Bind("priority", func() { t.togglePrio(t.current); t.fillDetails() }, "p").InGroup("Ticket")
	km.Bind("edit reply", func() {
		t.app.EditText("Reply to "+t.current.id, t.reply.Value(), func(s string) { t.reply.SetValue(s); t.replyFooter() })
	}, "ctrl+e").Hide().InGroup("Ticket")
	return page
}

func (t *ticketApp) back() {
	t.pages.Show("list")
	t.app.Focus(t.list)
}

func (t *ticketApp) open(k *ticket) {
	t.current = k
	t.threadP.SetTitleLine(tui.Line{{Text: k.id + " "}, {Text: k.title}})
	t.fillThread()
	t.fillDetails()
	t.reply.SetValue("")
	t.replyFooter()
	t.pages.Show("ticket")
	t.app.Focus(t.thread)
}

func (t *ticketApp) fillThread() {
	var lines []tui.Line
	for _, m := range t.current.messages {
		head := tui.Line{{Text: m.author, Style: theme.Bold}, {Text: " · " + m.at.Format("02.01. 15:04"), Style: theme.Fg256(244)}}
		if m.internal {
			head = append(head, tui.Span{Text: " "}, tui.Badge("internal", theme.Join(theme.Reverse, theme.Fg256(214))))
		}
		lines = append(lines, head)
		for l := range strings.SplitSeq(m.text, "\n") {
			style := theme.Style("")
			if m.internal {
				style = theme.Fg256(223)
			}
			lines = append(lines, tui.Line{{Text: "  " + l, Style: style}})
		}
		lines = append(lines, nil)
	}
	t.thread.SetLines(lines)
	t.thread.ScrollBottom()
}

func (t *ticketApp) fillDetails() {
	k := t.current
	val := func(s string) tui.Line { return tui.Text(s, "") }
	t.details.SetItems([]detailRow{
		{label: "Status", value: func() tui.Line { return tui.Text(k.status, statusStyles[k.status]) }, edit: func() { t.statusDialog(k) }},
		{label: "Assignee", value: func() tui.Line {
			if k.assignee == "" {
				return tui.Text("— (a to take)", theme.Fg256(244))
			}
			return val(k.assignee)
		}, edit: func() { t.assign(k) }},
		{label: "Priority", value: func() tui.Line {
			if k.high {
				return tui.Line{tui.Badge("high", theme.Join(theme.Reverse, theme.Fg256(203)))}
			}
			return val("normal")
		}, edit: func() { t.togglePrio(k) }},
		{label: "Department", value: func() tui.Line { return val(k.department) }, edit: func() {
			t.app.Choose("Department", departments, slices.Index(departments, k.department), func(i int) { k.department = departments[i] })
		}},
		{label: "Labels", value: func() tui.Line { return val(strings.Join(k.labels, ", ")) }, edit: func() { t.labelDialog(k) }},
		{label: "Resubmit", value: func() tui.Line {
			if k.resubmit == "" {
				return tui.Text("—", theme.Fg256(244))
			}
			return val(k.resubmit)
		}, edit: func() { t.resubmitDialog(k) }},
		{label: "Subscribed", value: func() tui.Line {
			if k.subscribed {
				return tui.Text("yes", theme.Fg256(114))
			}
			return tui.Text("no", theme.Fg256(244))
		}, edit: func() { k.subscribed = !k.subscribed }},
		{label: "Customer", value: func() tui.Line { return val(k.customer) }},
		{label: "Messages", value: func() tui.Line { return val(fmt.Sprint(len(k.messages))) }},
	})
}

func (t *ticketApp) replyFooter() {
	style, what := theme.Fg256(214), "internal note"
	if !t.internal.Value() {
		style, what = theme.Fg256(203), "visible to the customer"
	}
	t.replyP.SetTitleLine(tui.Line{{Text: "Reply "}, {Text: what, Style: style}})
	t.replyP.SetFooter(tui.Text(fmt.Sprintf("%d/%d", t.reply.Len(), t.reply.Limit()), theme.Fg256(244)))
}

func (t *ticketApp) send() {
	text := strings.TrimSpace(t.reply.Value())
	if text == "" {
		t.app.Notify(tui.LevelWarning, "Nothing to send")
		return
	}
	post := func() {
		done := t.app.Busy("sending…")
		k, internal := t.current, t.internal.Value()
		t.app.Go(func() func() {
			time.Sleep(500 * time.Millisecond)
			return func() {
				done()
				k.messages = append(k.messages, message{author: "me", at: time.Now(), internal: internal, text: text})
				if !internal {
					k.status = "answered"
				}
				k.updated = time.Now()
				t.refresh()
				// The user may have gone on meanwhile: only the draft sent
				// is cleared, and only the thread shown is filled again.
				if t.current != k {
					t.app.Notifyf(tui.LevelSuccess, "Sent to %s", k.id)
					return
				}
				if strings.TrimSpace(t.reply.Value()) == text {
					t.reply.SetValue("")
					t.replyFooter()
				}
				t.fillThread()
				t.app.Notify(tui.LevelSuccess, "Sent")
			}
		})
	}
	if t.internal.Value() {
		post()
		return
	}
	t.app.Confirm("Send to the customer", "The customer gets this reply by email. Send it?", post)
}
