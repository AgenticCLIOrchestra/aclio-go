// Package stream lets a consumer see, change, hold back or drop every log line
// aclio-go writes for a run.
//
// A run's lines reach the handler as Events, in order: one KindStart, then one
// KindStream per parsed CLI stream line (only when the run streams), then one
// KindUsage if aclio-go logs a usage line, and always exactly one KindEnd. Each
// Event carries the lines aclio-go would write for it (Lines); the handler
// returns the lines to write instead. Returning e.Lines keeps the default
// output.
//
// A HandlerFactory is called once at the start of each run, so every run gets
// its own Handler and its own state. One run's handler is called from one
// goroutine at a time, so it needs no locking; the HandlerFactory itself may be
// called by concurrent runs and must be safe for that.
//
// For claude stream events, Data holds the event decoded into the types in the
// claudeevent package.
package stream

import "encoding/json"

// Kind says where an Event comes from.
type Kind string

const (
	// KindStart is aclio-go's [start] line, logged just before the CLI is
	// launched.
	KindStart Kind = "start"
	// KindStream is one parsed line of the CLI's output stream. Sent only when
	// the run streams (Stream is true). A line that isn't valid JSON is
	// skipped and never sent.
	KindStream Kind = "stream"
	// KindUsage is aclio-go's usage line after the CLI exits: [cache] for
	// claude, [usage] for codex. Sent only when that line is logged.
	KindUsage Kind = "usage"
	// KindEnd is always the last event of a run, sent exactly once, whether
	// the run succeeded or not. Its Lines is empty; it lets a handler write out
	// anything it has held back.
	KindEnd Kind = "end"
)

// Provider is the agentic CLI a run drives. aclio.Provider is the same type.
type Provider string

const (
	ProviderClaude Provider = "claude"
	ProviderCodex  Provider = "codex"
)

// Event is one thing aclio-go is about to log for a run.
type Event struct {
	Kind     Kind
	Provider Provider
	Name     string // the run's name, "" if unnamed
	Prefix   string // the run's log prefix, e.g. "[claude] [worker]"

	// Type, Subtype, Raw and Data are set for KindStream only.

	// Type is the stream line's type, e.g. "assistant" or "system" for
	// claude, "item.completed" or "turn.completed" for codex. Plain strings:
	// the CLIs define these and add new ones without notice.
	Type string
	// Subtype is claude's subtype field (e.g. "init", "thinking_tokens"), or
	// for codex item.* events the item's type (e.g. "agent_message",
	// "command_execution"). Empty when there is none.
	Subtype string
	// Raw is the whole stream line. It is the handler's own copy, so it may
	// be kept.
	Raw json.RawMessage
	// Data is the stream line decoded, for claude: a claudeevent.AssistantEvent,
	// a claudeevent.UserEvent, or one of the claudeevent system event types
	// (claudeevent.InitEvent, ..., claudeevent.UnknownEvent). It is nil for
	// other claude types (e.g. "result"), for a line whose fields don't match
	// its type, and for codex.
	Data any

	// Lines are the lines aclio-go would write for this event, prefix
	// included and without trailing newlines. Empty when it would write
	// nothing (e.g. claude's "result" line, or KindEnd).
	Lines []string
}

// Handler is called with each Event of one run and returns the lines to write
// for it, in place of e.Lines. Each returned line is written as given, prefix
// and all, followed by a newline, in a single Write. Return e.Lines to keep
// the default output, nil to write nothing, or anything else, including lines
// held back from earlier events.
//
// aclio-go doesn't recover a panic in a Handler.
type Handler func(e Event) []string

// HandlerFactory is called once at the start of each run and returns that
// run's Handler. Returning nil means the run logs as if no handler were set.
type HandlerFactory func() Handler
