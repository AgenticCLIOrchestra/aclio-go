package cliinstall

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeBinary drops an executable named bin into dir.
func fakeBinary(t *testing.T, dir, bin string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake binary is a shell script")
	}
	path := filepath.Join(dir, bin)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFindPrefersPath(t *testing.T) {
	pathDir, defDir := t.TempDir(), t.TempDir()
	want := fakeBinary(t, pathDir, "tool")
	fakeBinary(t, defDir, "tool")
	t.Setenv("PATH", pathDir)

	if got, ok := Find("tool", defDir); !ok || got != want {
		t.Errorf("Find() = %q, %v, want %q, true", got, ok, want)
	}
}

func TestFindInstalledPrefersDir(t *testing.T) {
	pathDir, defDir := t.TempDir(), t.TempDir()
	fakeBinary(t, pathDir, "tool")
	want := fakeBinary(t, defDir, "tool")
	t.Setenv("PATH", pathDir)

	if got, ok := FindInstalled("tool", defDir); !ok || got != want {
		t.Errorf("FindInstalled() = %q, %v, want %q, true", got, ok, want)
	}
	if first := filepath.SplitList(os.Getenv("PATH"))[0]; first != defDir {
		t.Errorf("PATH[0] = %q, want %q", first, defDir)
	}
}

func TestFindMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	if got, ok := Find("tool", t.TempDir()); ok {
		t.Errorf("Find() = %q, true, want not found", got)
	}
	if got, ok := FindInDir("tool", ""); ok {
		t.Errorf("FindInDir(blank dir) = %q, true, want not found", got)
	}
}

func TestPrependPath(t *testing.T) {
	sep := string(os.PathListSeparator)
	t.Setenv("PATH", strings.Join([]string{"/a", "/b", "/c"}, sep))

	prependPath("/b")
	if got, want := os.Getenv("PATH"), strings.Join([]string{"/b", "/a", "/c"}, sep); got != want {
		t.Errorf("PATH = %q, want %q", got, want)
	}
}
