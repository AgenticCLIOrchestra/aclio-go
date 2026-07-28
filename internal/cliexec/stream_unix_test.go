//go:build unix

package cliexec

import (
	"os/exec"
	"strconv"
	"testing"
)

// TestStreamHandlesHugeLines — one stream-json event can be arbitrarily large
// (a claude tool_result echoing a lockfile-sized file); the old Scanner cap
// aborted such streams with "token too long", so Stream must read lines of
// any length and still deliver what follows them.
func TestStreamHandlesHugeLines(t *testing.T) {
	const lineLen = 12 * 1024 * 1024 // beyond the old 10MB scanner cap
	cmd := exec.Command("/bin/sh", "-c",
		"head -c "+strconv.Itoa(lineLen)+" /dev/zero | tr '\\0' a; echo; echo tail-line")

	var lines []string
	if err := Stream(cmd, func(line string) { lines = append(lines, line) }); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if got := len(lines[0]); got != lineLen {
		t.Errorf("huge line arrived with length %d, want %d", got, lineLen)
	}
	if lines[1] != "tail-line" {
		t.Errorf("line after the huge one = %q, want %q", lines[1], "tail-line")
	}
}
