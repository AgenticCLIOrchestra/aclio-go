// Package codexinstall installs the Codex CLI (`codex`) with OpenAI's
// official standalone installer, so the codex package (and aclio's codex
// provider) has a binary to run. It is opt-in: nothing in this module imports
// it, and nothing installs unless you call Ensure or Install.
//
// Typical use, before the first run:
//
//	if _, err := codexinstall.Ensure(codexinstall.Opts{}); err != nil { ... }
//	out, res, err := codex.Run(dir, opts)
package codexinstall

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

// Official installer scripts: install.sh on macOS/Linux, install.ps1 on
// native Windows. Both read the target version from CODEX_RELEASE.
const (
	scriptURLUnix    = "https://chatgpt.com/codex/install.sh"
	scriptURLWindows = "https://chatgpt.com/codex/install.ps1"
)

// binName is the CLI the codex package invokes.
const binName = "codex"

// versionRegex mirrors the targets the installer accepts: latest or a pinned
// version. Checked up front so a bad value fails before any download.
var versionRegex = regexp.MustCompile(`^(latest|[0-9]+\.[0-9]+\.[0-9]+(-alpha(\.[0-9]+){0,2}|-beta(\.[0-9]+)?)?)$`)

// Opts configures an install.
type Opts struct {
	// Version is the installer target: "latest" or a pinned x.y.z.
	// Blank means "latest" (the installer's default). Ensure ignores it when a
	// codex binary already exists.
	Version string
	// Output receives the installer's stdout and stderr. Nil defaults to
	// os.Stderr; pass io.Discard to silence it.
	Output io.Writer
}

// Ensure returns the path of an existing codex binary, installing it with
// Install only if none is found. A binary counts as existing when it is on
// PATH or in the standalone installer's default location; in the latter case
// that directory is prepended to this process's PATH so codex.Run finds it.
func Ensure(opts Opts) (string, error) {
	if path, ok := cliinstall.Find(binName, defaultDir()); ok {
		return path, nil
	}
	return Install(opts)
}

// Install runs the official installer unconditionally (re-installing or
// switching versions if codex is already present) and returns the installed
// binary's path. The installer's directory is moved to the front of this
// process's PATH, so codex.Run uses the fresh binary right after Install
// without a new shell.
func Install(opts Opts) (string, error) {
	if opts.Version != "" && !versionRegex.MatchString(opts.Version) {
		return "", fmt.Errorf("codexinstall: invalid version %q: want latest or x.y.z", opts.Version)
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
		return "", fmt.Errorf("codexinstall: %w", err)
	}
	defer os.Remove(script)

	// CODEX_NON_INTERACTIVE skips the installer's y/N prompts, which would
	// otherwise block on /dev/tty.
	env := append(os.Environ(), "CODEX_NON_INTERACTIVE=1")
	if opts.Version != "" {
		env = append(env, "CODEX_RELEASE="+opts.Version)
	}
	// sh: install.sh is POSIX sh.
	cmd := cliinstall.ScriptCommand("sh", script, nil)
	cmd.Env = env
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("codexinstall: installer failed: %w", err)
	}

	if path, ok := cliinstall.FindInstalled(binName, defaultDir()); ok {
		return path, nil
	}
	return "", errors.New("codexinstall: installer finished but no codex binary was found in " + defaultDir() + " or on PATH")
}

// defaultDir is where the standalone installer puts the codex binary:
// CODEX_INSTALL_DIR when set (the installer honors it too), else ~/.local/bin
// on unix and %LOCALAPPDATA%\Programs\OpenAI\Codex\bin on Windows.
func defaultDir() string {
	if dir := os.Getenv("CODEX_INSTALL_DIR"); dir != "" {
		return dir
	}
	if runtime.GOOS == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "Programs", "OpenAI", "Codex", "bin")
		}
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "bin")
}
