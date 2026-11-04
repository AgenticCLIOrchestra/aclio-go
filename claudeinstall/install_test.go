package claudeinstall

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVersionRegex(t *testing.T) {
	valid := []string{"stable", "latest", "2.1.89", "2.1.89-beta.1"}
	for _, v := range valid {
		if !versionRegex.MatchString(v) {
			t.Errorf("versionRegex(%q) = false, want true", v)
		}
	}

	invalid := []string{"", "2.1", "v2.1.89", "2.1.89; rm -rf /", "--help", "2.1.89 stable"}
	for _, v := range invalid {
		if versionRegex.MatchString(v) {
			t.Errorf("versionRegex(%q) = true, want false", v)
		}
	}
}

func TestInstallRejectsInvalidVersion(t *testing.T) {
	_, err := Install(Opts{Version: "--help"})
	if err == nil || !strings.Contains(err.Error(), "invalid version") {
		t.Fatalf("Install(--help) error = %v, want invalid version", err)
	}
}

// fakeBinary drops an executable named claude into dir.
func fakeBinary(t *testing.T, dir string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake binary is a shell script")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, binName)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEnsureFindsBinaryOnPath(t *testing.T) {
	binDir := t.TempDir()
	want := fakeBinary(t, binDir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", binDir)

	got, err := Ensure(Opts{})
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if got != want {
		t.Errorf("Ensure() = %q, want %q", got, want)
	}
}

func TestEnsureFindsBinaryInDefaultDir(t *testing.T) {
	home := t.TempDir()
	want := fakeBinary(t, filepath.Join(home, ".local", "bin"))
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())

	got, err := Ensure(Opts{})
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if got != want {
		t.Errorf("Ensure() = %q, want %q", got, want)
	}
	if first := filepath.SplitList(os.Getenv("PATH"))[0]; first != filepath.Dir(want) {
		t.Errorf("PATH[0] = %q, want %q", first, filepath.Dir(want))
	}
}
