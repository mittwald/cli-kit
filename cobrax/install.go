package cobrax

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

// installShells are the shells whose completion command AddCompletionInstall
// gives an --install flag: those with a directory per user that the shell
// loads completion scripts from.
var installShells = []string{"bash", "zsh", "fish"}

// AddCompletionInstall gives the bash, zsh and fish commands of completion,
// the command [HardenCompletion] returned, an --install flag. With it the
// command writes the script to the file [CompletionInstallPath] names for
// the shell, instead of to its output, and says where it went:
//
//	example completion bash --install
//	Installed bash completion to /home/me/.local/share/bash-completion/completions/example
//
// The file is replaced atomically: it is written next to its place and
// renamed, so a shell starting meanwhile reads the old script or the new
// one, never half of one. Missing directories are created. The script can
// be read by its owner only, which is all a shell running as that user
// needs. --no-descriptions applies as it does to a script that is printed.
// A failure to write the file is returned as a [*ScriptError].
//
// The flag is opt-in, so a tool decides whether its completion command
// writes files; without AddCompletionInstall the commands do what
// HardenCompletion made them do. A nil completion is ignored, so the result
// of HardenCompletion can be passed as it is. PowerShell gets no flag: it
// has no directory it loads scripts from, only a profile to source them in.
func AddCompletionInstall(completion *cobra.Command) {
	if completion == nil {
		return
	}
	program := completion.Root().Name()
	for _, shell := range completion.Commands() {
		name := shell.Name()
		generate := generator(completion.Root(), name)
		if generate == nil || !slices.Contains(installShells, name) {
			continue
		}
		install := shell.Flags().Bool("install", false, "write the script to the "+name+" completion directory of the current user")
		printScript := shell.RunE
		shell.RunE = func(cmd *cobra.Command, args []string) error {
			if !*install {
				if printScript == nil {
					return nil
				}
				return printScript(cmd, args)
			}
			path, err := installCompletion(cmd, program, name, generate)
			if err != nil {
				return &ScriptError{Shell: name, Err: err}
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Installed %s completion to %s\n", name, path); err != nil {
				return &ScriptError{Shell: name, Err: err}
			}
			return nil
		}
	}
}

// installCompletion writes the script for shell to its install path and
// returns the path.
func installCompletion(cmd *cobra.Command, program, shell string, generate func(*cobra.Command, io.Writer) error) (string, error) {
	path, err := CompletionInstallPath(program, shell)
	if err != nil {
		return "", err
	}
	var script bytes.Buffer
	if err := generate(cmd, &script); err != nil {
		return "", err
	}
	if err := replaceFile(path, script.Bytes()); err != nil {
		return "", err
	}
	return path, nil
}

// CompletionInstallPath returns the file a completion script for program is
// installed to for shell, for the current user:
//
//   - bash: program in the completions directory of bash-completion, under
//     the first absolute directory BASH_COMPLETION_USER_DIR lists, or else under
//     $XDG_DATA_HOME/bash-completion. bash-completion 2 loads it from there
//     the first time program is completed.
//   - zsh: _program in $XDG_DATA_HOME/zsh/site-functions. zsh loads it from
//     there only when that directory is on its fpath, before compinit runs.
//   - fish: program.fish in $XDG_CONFIG_HOME/fish/completions, which fish
//     loads from.
//
// XDG_DATA_HOME defaults to ~/.local/share and XDG_CONFIG_HOME to
// ~/.config; a value that is not an absolute path is ignored, as the XDG
// Base Directory specification asks. Any other shell, and a program name
// that is empty or holds a path separator, is an error.
func CompletionInstallPath(program, shell string) (string, error) {
	if program == "" || program == "." || program == ".." || strings.ContainsAny(program, `/\`) {
		return "", fmt.Errorf("completion install path: invalid program name %q", program)
	}
	switch shell {
	case "bash":
		if dir := firstAbsolute(os.Getenv("BASH_COMPLETION_USER_DIR")); dir != "" {
			return filepath.Join(dir, "completions", program), nil
		}
		data, err := xdgDir("XDG_DATA_HOME", ".local", "share")
		if err != nil {
			return "", err
		}
		return filepath.Join(data, "bash-completion", "completions", program), nil
	case "zsh":
		data, err := xdgDir("XDG_DATA_HOME", ".local", "share")
		if err != nil {
			return "", err
		}
		return filepath.Join(data, "zsh", "site-functions", "_"+program), nil
	case "fish":
		config, err := xdgDir("XDG_CONFIG_HOME", ".config")
		if err != nil {
			return "", err
		}
		return filepath.Join(config, "fish", "completions", program+".fish"), nil
	}
	return "", fmt.Errorf("completion install path: unsupported shell %q: want bash, zsh or fish", shell)
}

// firstAbsolute returns the first absolute path of a list of paths such as
// BASH_COMPLETION_USER_DIR, separated by colons (semicolons on Windows), or
// "".
func firstAbsolute(list string) string {
	for _, dir := range filepath.SplitList(list) {
		if dir = strings.TrimSpace(dir); filepath.IsAbs(dir) {
			return dir
		}
	}
	return ""
}

// xdgDir returns the directory the XDG variable env names when it is an
// absolute path, and else the home directory joined with fallback.
func xdgDir(env string, fallback ...string) (string, error) {
	if dir := strings.TrimSpace(os.Getenv(env)); filepath.IsAbs(dir) {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("completion install path: %w", err)
	}
	return filepath.Join(append([]string{home}, fallback...)...), nil
}

// replaceFile writes data to path through a temporary file in the same
// directory, renamed into place, creating the directory when it is missing.
// The file can be read by its owner only.
func replaceFile(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, removeIfPresent(tmp.Name()))
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// removeIfPresent removes path, and reports an error only when it is still
// there.
func removeIfPresent(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
