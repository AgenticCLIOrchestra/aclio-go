// Package aclio — Agentic CLI Orchestra — is a provider-agnostic front door
// for driving an agentic coding CLI from Go. Pick a Provider (Claude Code or
// Codex) and the same Request/Result types and Run/RunStructured functions
// route to the right underlying driver.
//
// The provider packages (claude, codex) are usable directly when you need
// provider-specific knobs the agnostic Request doesn't expose; the escape-hatch
// fields on Request (ClaudeOpts, CodexOpts) carry those through without leaving
// this layer.
//
//	res, err := aclio.Run(aclio.Request{
//	    Provider: aclio.Claude,
//	    Dir:      "/abs/workdir",
//	    Prompt:   "Summarize the open TODOs.",
//	})
//
// Logging (a process-wide sink via SetLogWriter, or a per-call
// Request.LogWriter, with every line passing through an optional event
// handler from SetEventHandlerFactory or Request.EventHandlerFactory), the
// ctrl+c behavior, and structured-output schema generation are shared across
// providers.
package aclio

import (
	"fmt"
	"io"

	"github.com/agenticcliorchestra/aclio-go/claude"
	"github.com/agenticcliorchestra/aclio-go/codex"
	"github.com/agenticcliorchestra/aclio-go/internal/cliexec"
	"github.com/agenticcliorchestra/aclio-go/jsonschema"
	"github.com/agenticcliorchestra/aclio-go/stream"
)

// ErrInterrupted is returned (wrapped) by Run / RunStructured when SIGINT or
// SIGTERM arrives while the CLI runs: the detached child is reaped and the call
// returns rather than exiting the process. Branch on it with
// errors.Is(err, aclio.ErrInterrupted).
var ErrInterrupted = cliexec.ErrInterrupted

// Provider selects which agentic CLI backs a request. It is the same type as
// stream.Provider, so it compares directly with Event.Provider.
type Provider = stream.Provider

const (
	Claude = stream.ProviderClaude
	Codex  = stream.ProviderCodex
)

// SetLogWriter sets the process-wide log sink (default os.Stderr) used by
// every call that doesn't set its own Request.LogWriter, including direct
// calls into the claude and codex packages. Pass io.Discard to silence the
// library, or nil to restore os.Stderr. It is safe to call while runs are in
// flight; a run already in progress keeps the writer it started with.
func SetLogWriter(w io.Writer) { cliexec.SetLogWriter(w) }

// Event is one thing aclio-go is about to log for a run; see the stream
// package.
type Event = stream.Event

// EventHandler gets every Event of one run and returns the lines to log for
// it; see the stream package.
type EventHandler = stream.Handler

// EventHandlerFactory returns a fresh EventHandler for one run; see the stream
// package.
type EventHandlerFactory = stream.HandlerFactory

// SetEventHandlerFactory sets the process-wide event handler factory used by
// every call that doesn't set its own Request.EventHandlerFactory, including
// direct calls into the claude and codex packages. The factory is called once
// at the start of each run. nil clears it, so lines are logged as they are. It
// is safe to call while runs are in flight; a run already in progress keeps
// the handler it started with.
func SetEventHandlerFactory(h EventHandlerFactory) { cliexec.SetEventHandlerFactory(h) }

// Request is a provider-agnostic invocation. The common fields map to each
// provider's equivalent; provider-specific extras ride along in ClaudeOpts /
// CodexOpts (only the non-shared fields of those are consulted — Prompt,
// Model, schema, resume id, name, streaming, the log writer, stderr and the
// event handler always come from this Request).
type Request struct {
	Provider Provider
	Dir      string
	Prompt   string
	// Model is the provider model string (Claude alias/ID, or Codex model
	// name). Empty leaves each provider on its default.
	Model string
	// JSONSchema constrains structured output. Usually left empty and filled
	// by RunStructured from the result type.
	JSONSchema string
	// ResumeID continues a prior session (Claude session id / Codex thread id).
	ResumeID string
	// Name is a short op label for the per-call log line.
	Name string
	// Stream logs events live as they arrive.
	Stream bool
	// LogWriter receives this call's log lines. nil uses the process-wide
	// sink set by SetLogWriter, so concurrent calls can each log to their own
	// writer. A writer shared between concurrent calls must be safe for
	// concurrent use (an *os.File is; a bytes.Buffer is not).
	LogWriter io.Writer
	// Stderr receives the CLI's own stderr (its warnings and errors, not this
	// library's log lines). nil means os.Stderr. A writer shared between
	// concurrent calls must be safe for concurrent use.
	Stderr io.Writer
	// EventHandlerFactory is called once at the start of this call and
	// returns the handler that sees, and may change, every line logged for it
	// (see the stream package). nil uses the process-wide factory set by
	// SetEventHandlerFactory; if neither is set, lines are logged as they are.
	EventHandlerFactory EventHandlerFactory

	// ClaudeOpts supplies Claude-only extras (AllowedTools, DisallowedTools,
	// SystemPromptFile, MaxTurns, ForkSession, TempDir). Consulted only when
	// Provider is Claude.
	ClaudeOpts *claude.RunOpts
	// CodexOpts supplies Codex-only extras (Sandbox, SkipGitRepoCheck,
	// Ephemeral, TempDir). Consulted only when Provider is Codex.
	CodexOpts *codex.RunOpts
}

// Result is the provider-agnostic outcome. Fields a provider doesn't report
// are zero (e.g. Codex reports no cost).
type Result struct {
	Provider     Provider
	Text         string
	SessionID    string
	CostUSD      float64
	InputTokens  int
	OutputTokens int
	CachedTokens int
}

// Run routes the request to the selected provider and returns a normalized
// Result.
func Run(req Request) (Result, error) {
	switch req.Provider {
	case Claude:
		out, err := claude.Run(req.Dir, claudeOpts(req))
		if err != nil {
			return Result{}, err
		}
		parsed, err := claude.ParseResult(out)
		if err != nil {
			return Result{}, fmt.Errorf("parsing claude output: %w", err)
		}
		return claudeResult(parsed), nil
	case Codex:
		text, result, err := codex.Run(req.Dir, codexOpts(req))
		if err != nil {
			return Result{}, err
		}
		result.FinalText = text
		return codexResult(result), nil
	default:
		return Result{}, fmt.Errorf("unknown provider %q", req.Provider)
	}
}

// RunStructured routes the request and decodes the provider's structured
// output into T. When req.JSONSchema is empty it is generated from T's json
// tags; set it explicitly for types a schema can't be derived from (e.g.
// discriminated unions behind interface fields).
func RunStructured[T any](req Request) (T, Result, error) {
	var zero T

	if req.JSONSchema == "" {
		schema, err := jsonschema.FromTypeJSON(zero)
		if err != nil {
			return zero, Result{}, fmt.Errorf("generating schema for %T: %w", zero, err)
		}
		req.JSONSchema = string(schema)
	}

	switch req.Provider {
	case Claude:
		v, parsed, err := claude.RunStructured[T](req.Dir, claudeOpts(req))
		if err != nil {
			return zero, Result{}, err
		}
		return v, claudeResult(parsed), nil
	case Codex:
		v, result, err := codex.RunStructured[T](req.Dir, codexOpts(req))
		if err != nil {
			return zero, Result{}, err
		}
		return v, codexResult(result), nil
	default:
		return zero, Result{}, fmt.Errorf("unknown provider %q", req.Provider)
	}
}

// claudeOpts maps the agnostic Request to claude.RunOpts, overlaying the
// Claude-only extras from req.ClaudeOpts when present.
func claudeOpts(req Request) claude.RunOpts {
	model := claude.Model(req.Model)
	if req.Model == "" {
		model = claude.Default
	}
	opts := claude.RunOpts{
		Prompt:              req.Prompt,
		JsonSchema:          req.JSONSchema,
		Name:                req.Name,
		ModelID:             model,
		Stream:              req.Stream,
		ResumeSessionID:     req.ResumeID,
		LogWriter:           req.LogWriter,
		Stderr:              req.Stderr,
		EventHandlerFactory: req.EventHandlerFactory,
	}
	if e := req.ClaudeOpts; e != nil {
		opts.AllowedTools = e.AllowedTools
		opts.DisallowedTools = e.DisallowedTools
		opts.SystemPromptFile = e.SystemPromptFile
		opts.MaxTurns = e.MaxTurns
		opts.ForkSession = e.ForkSession
		opts.TempDir = e.TempDir
	}
	return opts
}

// codexOpts maps the agnostic Request to codex.RunOpts, overlaying the
// Codex-only extras from req.CodexOpts when present.
func codexOpts(req Request) codex.RunOpts {
	opts := codex.RunOpts{
		Prompt:              req.Prompt,
		OutputSchema:        req.JSONSchema,
		Name:                req.Name,
		Model:               req.Model,
		Stream:              req.Stream,
		ResumeSessionID:     req.ResumeID,
		LogWriter:           req.LogWriter,
		Stderr:              req.Stderr,
		EventHandlerFactory: req.EventHandlerFactory,
	}
	if e := req.CodexOpts; e != nil {
		opts.Sandbox = e.Sandbox
		opts.SkipGitRepoCheck = e.SkipGitRepoCheck
		opts.Ephemeral = e.Ephemeral
		opts.TempDir = e.TempDir
	}
	return opts
}

func claudeResult(r *claude.RunResult) Result {
	return Result{
		Provider:     Claude,
		Text:         r.Result,
		SessionID:    r.SessionID,
		CostUSD:      r.TotalCostUSD,
		InputTokens:  r.Usage.InputTokens,
		OutputTokens: r.Usage.OutputTokens,
		CachedTokens: r.Usage.CacheReadInputTokens,
	}
}

func codexResult(r *codex.RunResult) Result {
	return Result{
		Provider:     Codex,
		Text:         r.FinalText,
		SessionID:    r.ThreadID,
		InputTokens:  r.Usage.InputTokens,
		OutputTokens: r.Usage.OutputTokens,
		CachedTokens: r.Usage.CachedInputTokens,
	}
}
