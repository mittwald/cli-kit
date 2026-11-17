package cobrax

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mittwald/cli-kit/fsx"
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
// needs. A symbolic link where the file belongs is refused rather than
// replaced, as [fsx.WriteFile] refuses it. --no-descriptions applies as it does to a script that is printed.
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
	if err := replaceFile(path, script.Bytes(), 0o750); err != nil {
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

// replaceFile writes data to path with [fsx.WriteFile], readable by its
// owner only, creating the directory with mode dirPerm when it is missing.
func replaceFile(path string, data []byte, dirPerm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return err
	}
	return fsx.WriteFile(path, data, 0o600)
}

// WriteCompletionScript writes the completion script of root for shell,
// one of bash, zsh, fish and powershell, to path. It is for a tool that
// keeps its script where an earlier version put it, such as a file in its
// configuration directory that the user's shell profile sources;
// [AddCompletionInstall] writes to the directory the shell loads scripts
// from by itself.
//
// The script carries descriptions unless
// root.CompletionOptions.DisableDescriptions is set. The file is replaced
// atomically, as AddCompletionInstall replaces it, and can be read by its
// owner only. Missing directories are created for their owner only (mode
// 0700), since the directory may be one the tool keeps other files in. A
// symbolic link at path is refused, as [fsx.WriteFile] refuses it.
//
// An unknown shell is an error; a failure to generate or write the script
// is returned as a [*ScriptError].
func WriteCompletionScript(root *cobra.Command, shell, path string) error {
	generate := generator(root, shell)
	if generate == nil {
		return fmt.Errorf("write completion script: unsupported shell %q: want bash, zsh, fish or powershell", shell)
	}
	var script bytes.Buffer
	if err := generate(root, &script); err != nil {
		return &ScriptError{Shell: shell, Err: err}
	}
	if err := replaceFile(path, script.Bytes(), 0o700); err != nil {
		return &ScriptError{Shell: shell, Err: err}
	}
	return nil
}
