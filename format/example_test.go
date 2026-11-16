package format_test

import (
	"fmt"
	"os"
	"time"

	"github.com/mittwald/cli-kit/format"
)

type release struct {
	Name    string   `json:"name"`
	Version string   `json:"version"`
	Stable  bool     `json:"stable"`
	Tags    []string `json:"tags,omitempty"`
}

func ExampleWriteYAML() {
	_ = format.WriteYAML(os.Stdout, release{Name: "cli-kit", Version: "0.1.0", Tags: []string{"go", "cli"}})
	// Output:
	// name: cli-kit
	// version: 0.1.0
	// stable: false
	// tags:
	//   - go
	//   - cli
}

func ExampleWriteJSON() {
	_ = format.WriteJSON(os.Stdout, release{Name: "cli-kit", Version: "0.1.0", Stable: true})
	// Output:
	// {
	//   "name": "cli-kit",
	//   "version": "0.1.0",
	//   "stable": true
	// }
}

func ExampleSet_Parse() {
	accepted := format.Set{format.Text, format.JSON, format.Format("ics")}
	f, err := accepted.Parse("ICS")
	fmt.Println(f, err)
	_, err = accepted.Parse("yaml")
	fmt.Println(err)
	// Output:
	// ics <nil>
	// unknown output format "yaml": want one of text, json, ics
}

func ExampleAge() {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	for _, t := range []time.Time{now, now.Add(-90 * time.Minute), now.AddDate(0, 0, -45), {}} {
		fmt.Printf("%q\n", format.Age(t, now))
	}
	// Output:
	// "now"
	// "1h"
	// "1mo"
	// ""
}
