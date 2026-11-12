package table

import (
	"bufio"
	"encoding/csv"
	"strings"
)

// renderRecords writes the header record, then one record per row: TSV for a
// tab delimiter, CSV with that separator otherwise. Every record has as many
// fields as the longest of them, the missing ones empty, since readers of
// records expect the same number in each.
func (t *Table) renderRecords() error {
	records := make([][]string, 0, len(t.rows)+1)
	if len(t.headers) > 0 {
		records = append(records, t.record(t.headers))
	}
	for _, row := range t.rows {
		records = append(records, t.record(row))
	}
	// A row without cells is a record of one empty field, like the row of
	// one empty cell.
	fields := 1
	for _, record := range records {
		fields = max(fields, len(record))
	}
	for i, record := range records {
		if len(record) < fields {
			records[i] = append(record, make([]string, fields-len(record))...)
		}
	}
	if t.delimiter == '\t' {
		return writeTSV(t, records)
	}
	return writeCSV(t, records)
}

// writeCSV writes one record per line, quoted as encoding/csv quotes, with
// LF line ends. A record of one empty field is written as "", a quoted empty
// field: encoding/csv would write an empty line, which readers of CSV skip,
// so the row would be gone.
func writeCSV(t *Table, records [][]string) error {
	w := bufio.NewWriter(t.w)
	writer := csv.NewWriter(w)
	writer.Comma = t.delimiter
	for _, record := range records {
		if len(record) == 1 && record[0] == "" {
			// The writer holds what it wrote before; flush it to keep the
			// order of the records.
			writer.Flush()
			if err := writer.Error(); err != nil {
				return err
			}
			if _, err := w.WriteString("\"\"\n"); err != nil {
				return err
			}
			continue
		}
		if err := writer.Write(record); err != nil {
			return err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}
	return w.Flush()
}

// writeTSV writes one record per line, fields separated by tabs and never
// quoted: TSV has no escape mechanism its consumers agree on, and a quoted
// field would be read literally by cut(1) or awk(1). A tab inside a value,
// which record leaves only where the line join brings one, is replaced by a
// space. A record of one empty field is an empty line, which TSV cannot
// write any other way: cut(1) and awk(1) count it, a reader that skips
// empty lines does not.
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
// then joined, so a record never spans two lines. With [EscapeFormulas], a
// field a spreadsheet would take for a formula gets a single quote in front.
func (t *Table) record(cells []string) []string {
	fields := make([]string, len(cells))
	for i, cell := range cells {
		field := plainCell(strings.ReplaceAll(cell, "\t", " "))
		field = strings.ReplaceAll(field, "\n", t.recordLineJoin)
		if t.escapeFormulas && formulaLike(cell, field) {
			field = "'" + field
		}
		fields[i] = field
	}
	return fields
}

// formulaLike reports whether a spreadsheet may take field, the record field
// made of cell, for a formula: cell starts with a tab or a carriage return,
// which a spreadsheet skips and a field no longer shows, or field starts,
// after any spaces, with "=", "+", "-" or "@" and is not just a number with
// a sign.
func formulaLike(cell, field string) bool {
	if cell != "" && (cell[0] == '\t' || cell[0] == '\r') {
		return true
	}
	field = strings.TrimLeft(field, " ")
	if field == "" {
		return false
	}
	switch field[0] {
	case '=', '@':
		return true
	case '+', '-':
		return !signedNumber(field)
	}
	return false
}

// signedNumber reports whether s is a sign followed by a decimal number and
// nothing else: digits with at least one of them, grouped by "." and ",".
func signedNumber(s string) bool {
	digits := 0
	for i := 1; i < len(s); i++ {
		switch c := s[i]; {
		case c >= '0' && c <= '9':
			digits++
		case c != '.' && c != ',':
			return false
		}
	}
	return digits > 0
}
