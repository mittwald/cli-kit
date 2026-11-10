// Package format names the output formats a command-line tool offers through
// an --output flag and writes the structured ones.
//
// A [Format] is a name such as "json" or "csv". A [Set] is the list of formats
// one command accepts; [Set.Parse] validates a flag value against it, and
// [Value] plugs that check into a flag parser. [Default] holds the formats
// this package knows how to classify: text, json, yaml, csv, tsv and
// markdown. A tool may add formats of its own, such as Format("ics"), to the
// set it accepts.
//
// [WriteJSON] and [WriteYAML] write the same view of a value. YAML is produced
// from the JSON encoding, so json struct tags are the single source of truth
// for both formats: field names, omitted fields, custom MarshalJSON methods
// and field order are the same in either output.
//
// Escape sequences belong in text output only. Markdown, CSV and TSV are
// pasted or parsed verbatim, and JSON and YAML are read by programs, so
// [Format.Colored] reports false for all of them whatever --color says.
package format
