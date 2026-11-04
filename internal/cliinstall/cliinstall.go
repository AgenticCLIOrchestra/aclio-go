// Package cliinstall holds the plumbing shared by claudeinstall and
// codexinstall: fetching an official installer script, running it with the
// platform's shell, and locating the installed binary (fixing up PATH so the
// provider packages can spawn it right away).
package cliinstall

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// downloadTimeout bounds the installer-script fetch only, not the install.
const downloadTimeout = 60 * time.Second

// DownloadScript fetches the installer at url into a temp file named after
// name, keeping url's extension (PowerShell refuses -File without .ps1), and
// returns its path. The caller removes it.
func DownloadScript(url, name string) (string, error) {
	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %s", url, resp.Status)
	}

	f, err := os.CreateTemp("", name+"-install-*"+filepath.Ext(url))
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("write temp file: %w", err)
	}
	return f.Name(), nil
}

// ScriptCommand runs the downloaded installer with the platform's shell:
// unixShell (bash, sh, ...) on unix, Windows PowerShell otherwise.
func ScriptCommand(unixShell, script string, args []string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("powershell",
			append([]string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script}, args...)...)
	}
	return exec.Command(unixShell, append([]string{script}, args...)...)
}

// Find locates bin on PATH, falling back to dir, the installer's default
// directory (which a fresh install may not have added to this process's PATH
// yet).
func Find(bin, dir string) (string, bool) {
	if path, err := exec.LookPath(bin); err == nil {
		return path, true
	}
	return FindInDir(bin, dir)
}

// FindInstalled is Find with the order flipped, for right after an install:
// the fresh copy in dir wins over any other bin already on PATH (an npm or
// Homebrew copy, say).
func FindInstalled(bin, dir string) (string, bool) {
	if path, ok := FindInDir(bin, dir); ok {
		return path, true
	}
	if path, err := exec.LookPath(bin); err == nil {
		return path, true
	}
	return "", false
}

// FindInDir looks for bin in dir only and, on a hit, makes sure dir leads this
// process's PATH so a bare bin resolves to it.
func FindInDir(bin, dir string) (string, bool) {
	if dir == "" {
		return "", false
	}
	path := filepath.Join(dir, exeName(bin))
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return "", false
	}
	prependPath(dir)
	return path, true
}

func exeName(bin string) string {
	if runtime.GOOS == "windows" {
		return bin + ".exe"
	}
	return bin
}

// prependPath moves dir to the front of this process's PATH (inherited by
// children it spawns afterwards), dropping any later duplicate of it.
func prependPath(dir string) {
	parts := []string{dir}
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p != dir && p != "" {
			parts = append(parts, p)
		}
	}
	os.Setenv("PATH", strings.Join(parts, string(os.PathListSeparator)))
}
