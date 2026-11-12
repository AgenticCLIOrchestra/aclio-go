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
	"sync/atomic"
)

// defaultWriter is the process-wide fallback log sink, used by every run that
// doesn't bring its own writer. Held atomically so SetLogWriter can be called
// while runs are in flight without a data race.
var defaultWriter atomic.Pointer[io.Writer]

func init() {
	SetLogWriter(nil)
}

// SetLogWriter sets the process-wide fallback log sink. nil restores the
// default, os.Stderr; io.Discard silences the library. Runs that set their own
// writer are unaffected. Safe to call concurrently with running calls; a run
// already in progress keeps the writer it started with.
func SetLogWriter(w io.Writer) {
	if w == nil {
		w = os.Stderr
	}
	defaultWriter.Store(&w)
}

// DefaultLogWriter returns the process-wide fallback log sink.
func DefaultLogWriter() io.Writer {
	return *defaultWriter.Load()
}

// Logger is where one run's log lines go and the tag each one starts with.
type Logger struct {
	Writer io.Writer
	Prefix string
}

// NewLogger builds the logger for one run: lines go to w, or to the
// process-wide sink (as it is now) when w is nil, each led by
// LogPrefix(provider, name).
func NewLogger(w io.Writer, provider, name string) Logger {
	if w == nil {
		w = DefaultLogWriter()
	}
	return Logger{Writer: w, Prefix: LogPrefix(provider, name)}
}

// Log writes one line to l.Writer: l.Prefix, a space, then format rendered
// with args. The line goes out in a single Write, so lines from runs sharing a
// writer don't tear into each other on an os.File. The prefix is never parsed
// as a format string, so a run name containing % is harmless.
func Log(l Logger, format string, args ...any) {
	fmt.Fprintf(l.Writer, "%s %s\n", l.Prefix, fmt.Sprintf(format, args...))
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
