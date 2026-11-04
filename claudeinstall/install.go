// Package claudeinstall installs the Claude Code CLI (`claude`) with
// Anthropic's official native installer, so the claude package (and aclio's
// claude provider) has a binary to run. It is opt-in: nothing in this module
// imports it, and nothing installs unless you call Ensure or Install.
//
// Typical use, before the first run:
//
//	if _, err := claudeinstall.Ensure(claudeinstall.Opts{}); err != nil { ... }
//	out, err := claude.Run(dir, opts)
package claudeinstall

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"

	"github.com/agenticcliorchestra/aclio-go/internal/cliinstall"
)

// Official installer scripts: install.sh on macOS/Linux/WSL, install.ps1 on
// native Windows. Each takes an optional target (stable, latest, or x.y.z) as
// its first argument.
const (
	scriptURLUnix    = "https://claude.ai/install.sh"
	scriptURLWindows = "https://claude.ai/install.ps1"
)

// binName is the CLI the claude package invokes.
const binName = "claude"

// versionRegex mirrors the targets the installer accepts: a release channel or
// a pinned version. Checked up front so a bad value fails before any download.
var versionRegex = regexp.MustCompile(`^(stable|latest|[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?)$`)

// Opts configures an install.
type Opts struct {
	// Version is the installer target: "stable", "latest", or a pinned x.y.z.
	// Blank means "latest" (the installer's default). Ensure ignores it when a
	// claude binary already exists.
	Version string
	// Output receives the installer's stdout and stderr. Nil defaults to
	// os.Stderr; pass io.Discard to silence it.
	Output io.Writer
}

// Ensure returns the path of an existing claude binary, installing it with
// Install only if none is found. A binary counts as existing when it is on
// PATH or in the native installer's default location; in the latter case
// that directory is prepended to this process's PATH so claude.Run finds it.
func Ensure(opts Opts) (string, error) {
	if path, ok := cliinstall.Find(binName, defaultDir()); ok {
		return path, nil
	}
	return Install(opts)
}

// Install runs the official installer unconditionally (re-installing or
// switching versions if claude is already present) and returns the installed
// binary's path. The installer's directory is moved to the front of this
// process's PATH, so claude.Run uses the fresh binary right after Install
// without a new shell.
func Install(opts Opts) (string, error) {
	if opts.Version != "" && !versionRegex.MatchString(opts.Version) {
		return "", fmt.Errorf("claudeinstall: invalid version %q: want stable, latest, or x.y.z", opts.Version)
	}
	out := opts.Output
	if out == nil {
		out = os.Stderr
	}

	scriptURL := scriptURLUnix
	if runtime.GOOS == "windows" {
		scriptURL = scriptURLWindows
	}

	script, err := cliinstall.DownloadScript(scriptURL, binName)
	if err != nil {
		return "", fmt.Errorf("claudeinstall: %w", err)
	}
	defer os.Remove(script)

	var args []string
	if opts.Version != "" {
		args = append(args, opts.Version)
	}
	// bash, not sh: install.sh uses bash features.
	cmd := cliinstall.ScriptCommand("bash", script, args)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claudeinstall: installer failed: %w", err)
	}

	if path, ok := cliinstall.FindInstalled(binName, defaultDir()); ok {
		return path, nil
	}
	return "", errors.New("claudeinstall: installer finished but no claude binary was found in " + defaultDir() + " or on PATH")
}

// defaultDir is where the native installer puts the claude launcher:
// ~/.local/bin on every platform (claude.exe on Windows).
func defaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "bin")
}
