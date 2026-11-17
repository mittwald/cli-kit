package fsx_test

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mittwald/cli-kit/fsx"
)

func ExampleWriteFile() {
	dir, err := os.MkdirTemp("", "example")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "config.yaml")
	if err := fsx.WriteFile(path, []byte("theme: dark\n"), 0o644); err != nil {
		panic(err)
	}
	data, _ := os.ReadFile(path)
	fmt.Print(string(data))
	// Output:
	// theme: dark
}
