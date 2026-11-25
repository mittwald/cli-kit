// Package notice writes the error, warning, info, OK and debug lines of a
// command-line tool: one line per notice on the error stream, labeled
// ERROR, WARN, INFO, OK or DEBUG.
//
// A [Theme] sets how a notice looks. [Line] writes the label in its level's
// color and a colon ("WARN: …"); [Badge] writes the label on its level's
// background, padded so the messages line up; [Bar] adds a tint of the
// level's color behind the message, and [Block] puts the whole line on the
// level's background. Bar and Block fill the line to the right edge of the
// terminal unless the theme says otherwise.
//
// The colors of a theme are [Colors]: a [Palette] for dark terminals and
// one for light ones, picked by [Options] Background, which reads
// COLORFGBG by default. [Default], [Muted], [Deep] and [Pastel] are the
// built-in colors; [Theme.WithColors] puts any of them on any theme, and
// [Options] Colors change single colors over them.
//
// Values in a message are marked with a [Role], so they stand out in a
// color of their own: [Date], [Time], [Detail], [Ref] and [Path] mark the
// built-in roles, and [NewRole] makes one of a tool's own, with colors for
// both backgrounds. A [Pattern] gives a role to whatever a regular
// expression matches in the message's own text, such as a merge request
// reference.
//
// Every notice is made safe before it is written: escape sequences and
// control characters are taken out (term.Plain) and recognizable
// credentials replaced (redact.Patterns), in the message and in its values.
// Credentials are looked for in the message as it reads, values in place;
// a notice in which one is found is written without colored values.
// Where the writer takes no color, as term.Colored decides, a notice is
// plain text in every theme: "WARN: …".
//
// A [Printer] writes notices with its [Options]. The package-level
// functions, such as [Warnf], use the Printer that [Use] installed for the
// whole process, or a [Line] Printer on os.Stderr.
package notice
