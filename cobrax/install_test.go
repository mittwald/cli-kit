package cobrax

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// isolateHome points the home and XDG directories at a fresh directory.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("BASH_COMPLETION_USER_DIR", "")
	return home
}

func TestCompletionInstallPath(t *testing.T) {
	home := isolateHome(t)
	data := filepath.Join(home, "data")
	config := filepath.Join(home, "config")
	userDir := filepath.Join(home, "bash-user")
	list := strings.Join([]string{"relative", userDir, filepath.Join(home, "second")}, string(os.PathListSeparator))
	tests := []struct {
		name  string
		shell string
		env   map[string]string
		want  string
	}{
		{"bash default", "bash", nil, filepath.Join(home, ".local", "share", "bash-completion", "completions", "example")},
		{"bash xdg", "bash", map[string]string{"XDG_DATA_HOME": data}, filepath.Join(data, "bash-completion", "completions", "example")},
		{"bash user dir", "bash", map[string]string{"XDG_DATA_HOME": data, "BASH_COMPLETION_USER_DIR": list}, filepath.Join(userDir, "completions", "example")},
		{"bash relative user dir ignored", "bash", map[string]string{"BASH_COMPLETION_USER_DIR": "relative"}, filepath.Join(home, ".local", "share", "bash-completion", "completions", "example")},
		{"zsh default", "zsh", nil, filepath.Join(home, ".local", "share", "zsh", "site-functions", "_example")},
		{"zsh xdg", "zsh", map[string]string{"XDG_DATA_HOME": data}, filepath.Join(data, "zsh", "site-functions", "_example")},
		{"zsh relative xdg ignored", "zsh", map[string]string{"XDG_DATA_HOME": "data"}, filepath.Join(home, ".local", "share", "zsh", "site-functions", "_example")},
		{"fish default", "fish", nil, filepath.Join(home, ".config", "fish", "completions", "example.fish")},
		{"fish xdg", "fish", map[string]string{"XDG_CONFIG_HOME": config}, filepath.Join(config, "fish", "completions", "example.fish")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			got, err := CompletionInstallPath("example", tt.shell)
			if err != nil || got != tt.want {
				t.Errorf("CompletionInstallPath(%q) = %q, %v; want %q", tt.shell, got, err, tt.want)
			}
		})
	}
	for _, bad := range []struct{ program, shell string }{
		{"example", "powershell"}, {"example", "tcsh"},
		{"", "bash"}, {"..", "bash"}, {"a/b", "zsh"}, {`a\b`, "fish"},
	} {
		if got, err := CompletionInstallPath(bad.program, bad.shell); err == nil {
			t.Errorf("CompletionInstallPath(%q, %q) = %q, want an error", bad.program, bad.shell, got)
		}
	}
}

func TestAddCompletionInstall(t *testing.T) {
	for _, shell := range installShells {
		t.Run(shell, func(t *testing.T) {
			isolateHome(t)
			root := newRoot()
			AddCompletionInstall(HardenCompletion(root))
			printed, err := execute(t, root, "completion", shell)
			if err != nil || !strings.Contains(printed, "__complete") {
				t.Fatalf("without --install: err %v, output %.80q", err, printed)
			}

			root = newRoot()
			AddCompletionInstall(HardenCompletion(root))
			out, err := execute(t, root, "completion", shell, "--install")
			if err != nil {
				t.Fatal(err)
			}
			path, _ := CompletionInstallPath("example", shell)
			if want := "Installed " + shell + " completion to " + path + "\n"; out != want {
				t.Errorf("output = %q, want %q", out, want)
			}
			installed, err := os.ReadFile(path)
			if err != nil || string(installed) != printed {
				t.Fatalf("installed script differs from the printed one (err %v)", err)
			}
			if runtime.GOOS != "windows" {
				if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
					t.Errorf("mode = %v, %v; want 0600", info.Mode().Perm(), err)
				}
			}

			// Installing again replaces the file and leaves nothing behind.
			root = newRoot()
			AddCompletionInstall(HardenCompletion(root))
			if _, err := execute(t, root, "completion", shell, "--install", "--no-descriptions"); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(entries) != 1 {
				t.Errorf("directory holds %v, %v; want the script alone", entries, err)
			}
		})
	}
}

func TestAddCompletionInstallFailure(t *testing.T) {
	home := isolateHome(t)
	// A file where the directory should be makes the install fail.
	blocker := filepath.Join(home, "blocked")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", blocker)
	root := newRoot()
	AddCompletionInstall(HardenCompletion(root))
	_, err := execute(t, root, "completion", "zsh", "--install")
	var scriptErr *ScriptError
	if !errors.As(err, &scriptErr) || scriptErr.Shell != "zsh" {
		t.Fatalf("err = %v, want a *ScriptError", err)
	}
}

// Only the shells with a directory of their own get the flag, and a missing
// completion command is no error.
func TestAddCompletionInstallFlags(t *testing.T) {
	root := newRoot()
	completion := HardenCompletion(root)
	AddCompletionInstall(completion)
	for _, shell := range completion.Commands() {
		has := shell.Flags().Lookup("install") != nil
		if want := shell.Name() != "powershell"; has != want {
			t.Errorf("%s: --install = %v, want %v", shell.Name(), has, want)
		}
	}
	AddCompletionInstall(nil)
	bare := &cobra.Command{Use: "example", RunE: func(*cobra.Command, []string) error { return nil }}
	AddCompletionInstall(HardenCompletion(bare))
}
