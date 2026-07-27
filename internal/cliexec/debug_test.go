package cliexec

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDebugStampFormat — the stamp is millisecond-precise and round-trips
// through its own layout, so lexicographic order is chronological order.
func TestDebugStampFormat(t *testing.T) {
	stamp := DebugStamp()
	if _, err := time.Parse("20060102-150405.000", stamp); err != nil {
		t.Errorf("DebugStamp() = %q, not in layout 20060102-150405.000: %v", stamp, err)
	}
}

// TestDebugName — call names are normalised for filenames, never used raw:
// lowercased, anything outside [a-z0-9_-] mapped to '-', empty → "unnamed".
func TestDebugName(t *testing.T) {
	cases := map[string]string{
		"":             "unnamed",
		"interaction":  "interaction",
		"My Op":        "my-op",
		"my op/name!":  "my-op-name-",
		"weird\tname":  "weird-name",
		"Mixed_OK-42":  "mixed_ok-42",
		"../../escape": "------escape",
	}
	for in, want := range cases {
		if got := DebugName(in); got != want {
			t.Errorf("DebugName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestWriteDebugFileCreatesDir — the debug dir is created lazily, so dumps
// into a not-yet-existing TempDir aren't silently dropped.
func TestWriteDebugFileCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dbg")
	WriteDebugFile(dir, "x.txt", []byte("hi"))
	data, err := os.ReadFile(filepath.Join(dir, "x.txt"))
	if err != nil {
		t.Fatalf("dump not written into missing dir: %v", err)
	}
	if string(data) != "hi" {
		t.Errorf("dump content = %q, want %q", data, "hi")
	}
}

// TestWriteDebugFileEmptyDirNoop — an empty dir disables dumps entirely.
func TestWriteDebugFileEmptyDirNoop(t *testing.T) {
	WriteDebugFile("", "x.txt", []byte("hi")) // must not panic or write anywhere
}
