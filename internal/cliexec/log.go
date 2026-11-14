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

	"github.com/agenticcliorchestra/aclio-go/stream"
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

// defaultHandlerFactory is the process-wide fallback event handler factory,
// used by every run that doesn't bring its own. nil (the zero value) means
// none.
var defaultHandlerFactory atomic.Pointer[stream.HandlerFactory]

// SetEventHandlerFactory sets the process-wide fallback event handler factory.
// nil clears it. Runs that set their own are unaffected. Safe to call
// concurrently with running calls; a run already in progress keeps the handler
// it started with.
func SetEventHandlerFactory(h stream.HandlerFactory) {
	if h == nil {
		defaultHandlerFactory.Store(nil)
		return
	}
	defaultHandlerFactory.Store(&h)
}

// DefaultEventHandlerFactory returns the process-wide fallback event handler
// factory, or nil when none is set.
func DefaultEventHandlerFactory() stream.HandlerFactory {
	if h := defaultHandlerFactory.Load(); h != nil {
		return *h
	}
	return nil
}

// Logger is where one run's log lines go, the tag each one starts with, and
// the run's event handler, if any.
type Logger struct {
	Writer   io.Writer
	Prefix   string
	Provider stream.Provider
	Name     string
	// Handler sees every event before it is written (see Emit). nil writes
	// each event's lines as they are.
	Handler stream.Handler
}

// NewLogger builds the logger for one run: lines go to w, or to the
// process-wide sink (as it is now) when w is nil, each led by
// LogPrefix(provider, name). The run's handler comes from factory, or from
// the process-wide factory (as it is now) when factory is nil; the factory
// is called here, once per run.
func NewLogger(w io.Writer, provider stream.Provider, name string, factory stream.HandlerFactory) Logger {
	if w == nil {
		w = DefaultLogWriter()
	}
	if factory == nil {
		factory = DefaultEventHandlerFactory()
	}
	var h stream.Handler
	if factory != nil {
		h = factory()
	}
	return Logger{Writer: w, Prefix: LogPrefix(string(provider), name), Provider: provider, Name: name, Handler: h}
}

// Line renders one log line: l.Prefix, a space, then format rendered with
// args. The prefix is never parsed as a format string, so a run name
// containing % is harmless.
func Line(l Logger, format string, args ...any) string {
	return l.Prefix + " " + fmt.Sprintf(format, args...)
}

// Log writes one line, rendered by Line, straight to l.Writer without
// involving the handler.
func Log(l Logger, format string, args ...any) {
	WriteLines(l, []string{Line(l, format, args...)})
}

// WriteLines writes each line to l.Writer followed by a newline. Each line
// goes out in a single Write, so lines from runs sharing a writer don't tear
// into each other on an os.File.
func WriteLines(l Logger, lines []string) {
	for _, line := range lines {
		_, _ = io.WriteString(l.Writer, line+"\n")
	}
}

// Emit sends e to the run's handler with its provider, name, prefix and lines
// filled in, then writes whatever lines the handler returns. Without a handler
// it writes lines as they are, and no Event is built.
func Emit(l Logger, e stream.Event, lines []string) {
	if l.Handler == nil {
		WriteLines(l, lines)
		return
	}
	e.Provider = l.Provider
	e.Name = l.Name
	e.Prefix = l.Prefix
	e.Lines = lines
	WriteLines(l, l.Handler(e))
}

// EmitStream emits one parsed stream line: Raw is the handler's own copy of
// raw, made only when there is a handler.
func EmitStream(l Logger, typ, subtype, raw string, data any, lines []string) {
	e := stream.Event{Kind: stream.KindStream, Type: typ, Subtype: subtype, Data: data}
	if l.Handler != nil {
		e.Raw = []byte(raw) // the conversion copies
	}
	Emit(l, e, lines)
}

// End sends the run's final KindEnd event and writes any lines the handler
// returns. It must be called exactly once per run, after everything else the
// run logs.
func End(l Logger) {
	Emit(l, stream.Event{Kind: stream.KindEnd}, nil)
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
