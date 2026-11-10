package format

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/goccy/go-yaml"
)

// ErrNotStructured is returned by [Write] for a format other than JSON or
// YAML.
var ErrNotStructured = errors.New("format is not structured")

// Write writes v in format f, which must be [JSON] or [YAML].
func Write(w io.Writer, f Format, v any) error {
	switch f {
	case JSON:
		return WriteJSON(w, v)
	case YAML:
		return WriteYAML(w, v)
	}
	return fmt.Errorf("%w: %q", ErrNotStructured, f)
}

// WriteJSON writes v as JSON indented by two spaces, followed by a newline.
// Unlike json.MarshalIndent it leaves <, > and & as they are: the output is
// read in a terminal or by a program, not embedded in HTML.
func WriteJSON(w io.Writer, v any) error {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(v); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// WriteYAML writes the view [WriteJSON] writes, as YAML. The value goes
// through its JSON encoding first, so the json tags stay the single source of
// truth for field names, omitted fields and the formatting custom
// MarshalJSON methods apply. The JSON is then decoded into an ordered map, so
// fields keep the order the struct defines instead of being sorted.
//
// Scalars whose plain YAML form would read back as another type (a time, a
// date, a boolean, a number, null) are quoted, and sequences are indented
// under their key.
func WriteYAML(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var document any
	if err := yaml.UnmarshalWithOptions(data, &document, yaml.UseOrderedMap()); err != nil {
		return err
	}
	out, err := yaml.MarshalWithOptions(document, yaml.IndentSequence(true))
	if err != nil {
		return err
	}
	_, err = w.Write(out)
	return err
}
