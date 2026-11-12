package table

import (
	"bufio"
	"encoding/csv"
	"strings"
)

// renderRecords writes the header record, then one record per row: TSV for a
// tab delimiter, CSV with that separator otherwise.
func (t *Table) renderRecords() error {
	records := make([][]string, 0, len(t.rows)+1)
	if len(t.headers) > 0 {
		records = append(records, t.record(t.headers))
	}
	for _, row := range t.rows {
		records = append(records, t.record(row))
	}
	if t.delimiter == '\t' {
		return writeTSV(t, records)
	}
	writer := csv.NewWriter(t.w)
	writer.Comma = t.delimiter
	for _, record := range records {
		if err := writer.Write(record); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

// writeTSV writes one record per line, fields separated by tabs and never
// quoted: TSV has no escape mechanism its consumers agree on, and a quoted
// field would be read literally by cut(1) or awk(1). A tab inside a value,
// which record leaves only where the line join brings one, is replaced by a
// space.
func writeTSV(t *Table, records [][]string) error {
	w := bufio.NewWriter(t.w)
	for _, fields := range records {
		for i, field := range fields {
			fields[i] = strings.ReplaceAll(field, "\t", " ")
		}
		if _, err := w.WriteString(strings.Join(fields, "\t") + "\n"); err != nil {
			return err
		}
	}
	return w.Flush()
}

// record turns cells into fields that carry no control character: every
// carriage return becomes a line break, a tab a space, and the cell is then
// made plain text, as term.Plain does — escape sequences removed, since a
// caller may still hand over a painted cell, and with them every other
// control character and invalid UTF-8. The lines of a multi-line cell are
// then joined, so a record never spans two lines.
func (t *Table) record(cells []string) []string {
	fields := make([]string, len(cells))
	for i, cell := range cells {
		field := plainCell(strings.ReplaceAll(cell, "\t", " "))
		fields[i] = strings.ReplaceAll(field, "\n", t.recordLineJoin)
	}
	return fields
}
