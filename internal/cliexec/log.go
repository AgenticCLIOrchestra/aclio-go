// Package cliexec is the shared machinery for driving an agentic CLI as a
// subprocess: detached (own-session) process spawning so the CLI can't grab
// the caller's terminal, a ctrl+c guard, stdout capture and line-streaming,
// greppable logging, and best-effort debug dumps. It is provider-agnostic —
// the claude and codex packages build their own args and parse their own
// event streams on top of it.
package cliexec

import (
	"fmt"
	"io"
	"os"
)

// LogWriter receives all log lines emitted by this library. Defaults to
// stderr; set to io.Discard to silence the library, or to a custom writer to
// redirect.
var LogWriter io.Writer = os.Stderr

// Logf writes one line to LogWriter.
func Logf(format string, args ...any) {
	fmt.Fprintf(LogWriter, format+"\n", args...)
}

// LogPrefix builds the leading tag every log line for a run starts with:
// "[provider]", or "[provider] [name]" when name is non-empty — so the lines
// of a named run can be filtered by name (e.g. a workflow step) as well as by
// provider, even when several runs interleave on the same writer.
func LogPrefix(provider, name string) string {
	if name == "" {
		return "[" + provider + "]"
	}
	return "[" + provider + "] [" + name + "]"
}

// Truncate shortens s to max runes-ish (bytes) with an ellipsis, for log
// lines that shouldn't dump full tool inputs or message bodies.
func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
