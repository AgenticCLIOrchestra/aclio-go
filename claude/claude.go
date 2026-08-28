// Package claude drives the Claude Code CLI (`claude -p`) as a subprocess:
// single-shot or streaming runs, structured output backed by a JSON schema,
// greppable [claude]-prefixed logging of stream events and cache metrics, and
// (via internal/cliexec) a ctrl+c guard that keeps the terminal responsive
// while the CLI runs detached in its own session.
package claude

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/agenticcliorchestra/aclio-go/internal/cliexec"
)

// ErrInterrupted is returned (wrapped) by Run when SIGINT/SIGTERM arrives while
// the CLI runs: the detached child is reaped and the call returns rather than
// exiting the process. Branch on it with errors.Is(err, claude.ErrInterrupted).
var ErrInterrupted = cliexec.ErrInterrupted

// allowedToolRegex accepts tool names like Bash, WebFetch(domain:...), or
// mcp__server__tool. It's a shape check, not a whitelist: the goal is to keep
// the value from smuggling extra CLI flags, not to track the CLI's tool set.
var allowedToolRegex = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\(.*\))?$`)

func isValidAllowedTool(tool string) bool {
	return allowedToolRegex.MatchString(tool)
}

// RunOpts configures one claude CLI invocation.
type RunOpts struct {
	// Prompt is fed to the CLI via stdin (never argv), so payload-carrying
	// prompts don't hit OS argument-size limits.
	Prompt string `json:"prompt"`
	// JsonSchema is passed to --json-schema; the result's structured_output
	// then conforms to it. RunStructured fills this from the result type.
	JsonSchema string `json:"json_schema"`
	// Name is the op label in the per-call [claude] [cache] log line, keeping
	// per-workflow cache hits greppable. Blank defaults to "unnamed".
	Name string `json:"name"`
	// AllowedTools maps to repeated --allowedTools flags; values are
	// shape-checked so one can't smuggle extra CLI flags.
	AllowedTools []string `json:"allowed_tools"`
	// DisallowedTools maps to --disallowedTools; the CLI evaluates deny rules
	// before allow rules.
	DisallowedTools []string `json:"disallowed_tools"`
	// MaxTurns maps to --max-turns; 0 omits the flag (the CLI's default).
	MaxTurns int `json:"max_turns"`
	// ModelID maps to --model: an alias or a pinned claude-* ID, validated by
	// IsValidModel.
	ModelID Model `json:"model_id"`
	// SystemPromptFile maps to --system-prompt-file. Absolute path only — a
	// relative one would resolve against absDir, not the caller's CWD.
	SystemPromptFile string `json:"system_prompt_file"`
	// Stream selects stream-json output with live [claude] event logging;
	// otherwise one JSON result blob is captured silently.
	Stream bool `json:"stream"`
	// ResumeSessionID maps to --resume: a prior result's SessionID, continuing
	// that conversation instead of starting fresh.
	ResumeSessionID string `json:"resume_session_id"`
	// ForkSession maps to --fork-session: the resumed conversation continues
	// under a new session id, leaving the original session untouched. Valid
	// only alongside ResumeSessionID — there is no session to fork otherwise,
	// and buildArgs rejects it.
	ForkSession bool `json:"fork_session"`
	// TempDir, when set, receives per-call debug dumps (settings, prompt,
	// output, error) as {stamp}-{name}-{kind}, stamped so calls never
	// overwrite each other. Empty disables the dumps.
	TempDir string `json:"-"`
}

// Run invokes the claude CLI in absDir and returns its result blob (JSON in
// both output modes; the last stream-json line when opts.Stream is set).
// Decode it with ParseResult or Structured.
//
// If SIGINT/SIGTERM arrives while the CLI runs, Run reaps the detached child
// and returns an error wrapping ErrInterrupted rather than exiting the process;
// branch on it with errors.Is(err, claude.ErrInterrupted).
func Run(absDir string, opts RunOpts) (string, error) {
	stamp := cliexec.DebugStamp()

	args, err := buildArgs(opts)
	if err != nil {
		debugError(opts.TempDir, stamp, opts.Name, err)
		return "", err
	}

	logStart(opts)
	debugSettings(opts.TempDir, stamp, opts)
	debugPrompt(opts.TempDir, stamp, opts.Name, opts.Prompt)

	cmd := cliexec.Command(absDir, "claude", args)
	// The prompt travels via stdin, not as a `-p` argument: a single argv
	// element is capped at ~128 KB on Linux (MAX_ARG_STRLEN) and the whole
	// argv+env near ~1 MB elsewhere, and payload-carrying prompts blow past
	// that ("argument list too long"). The bare `-p` flag (see buildArgs)
	// keeps the CLI in non-interactive print mode, reading the prompt from
	// stdin.
	cmd.Stdin = strings.NewReader(opts.Prompt)

	var out string
	if opts.Stream {
		out, err = runStream(cmd)
	} else {
		out, err = cliexec.Capture(cmd)
	}

	logCacheMetrics(opts.Name, out, err)
	if out != "" {
		debugOut(opts.TempDir, stamp, opts.Name, out)
	}
	debugError(opts.TempDir, stamp, opts.Name, err)

	return out, err
}

// buildArgs assembles the CLI arguments from opts, validating the model and
// tools before anything is spawned.
func buildArgs(opts RunOpts) ([]string, error) {
	outputFormat := "json"
	if opts.Stream {
		outputFormat = "stream-json"
	}

	// Bare -p (no value): print mode; the prompt is fed on stdin by Run.
	args := []string{"-p", "--output-format", outputFormat}

	if opts.ForkSession && opts.ResumeSessionID == "" {
		return nil, errors.New("ForkSession requires ResumeSessionID: there is no session to fork")
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
		if opts.ForkSession {
			args = append(args, "--fork-session")
		}
	}
	if opts.Stream {
		args = append(args, "--verbose")
	}
	if opts.JsonSchema != "" {
		args = append(args, "--json-schema", opts.JsonSchema)
	}
	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", opts.MaxTurns))
	}
	if opts.SystemPromptFile != "" {
		args = append(args, "--system-prompt-file", opts.SystemPromptFile)
	}

	if !IsValidModel(opts.ModelID) {
		return nil, errors.New("invalid model ID: " + string(opts.ModelID))
	}
	args = append(args, "--model", string(opts.ModelID))

	for _, tool := range opts.AllowedTools {
		if !isValidAllowedTool(tool) {
			return nil, errors.New("invalid allowed tool: " + tool)
		}
		args = append(args, "--allowedTools", tool)
	}
	for _, tool := range opts.DisallowedTools {
		if !isValidAllowedTool(tool) {
			return nil, errors.New("invalid disallowed tool: " + tool)
		}
		args = append(args, "--disallowedTools", tool)
	}

	return args, nil
}

// logStart writes one greppable [claude] [start] line per call, announcing
// the spawn. It pairs with the [claude] [cache] completion line via the
// shared op name; the prompt size is an early tell for runaway payload
// growth. Emitted only after arg validation, so a start line means a process
// was actually launched.
func logStart(opts RunOpts) {
	op := opts.Name
	if op == "" {
		op = "unnamed"
	}
	cliexec.Logf("[claude] [start] op=%s model=%s prompt_chars=%d", op, opts.ModelID, len(opts.Prompt))
}

// logCacheMetrics writes one greppable [claude] [cache] line per call.
// Best-effort: the CLI's result blob is JSON-shaped in both --output-format
// modes (the last stream-json line for stream mode), so we just try to parse
// and silently bail on anything unexpected.
func logCacheMetrics(name, output string, runErr error) {
	if runErr != nil || output == "" {
		return
	}
	op := name
	if op == "" {
		op = "unnamed"
	}
	parsed, err := ParseResult(output)
	if err != nil {
		return
	}
	u := parsed.Usage
	// A zero-usage result usually means we parsed an event that isn't the
	// CLI's result blob (shouldn't happen in either mode, but harmless).
	if u.InputTokens == 0 && u.OutputTokens == 0 &&
		u.CacheReadInputTokens == 0 && u.CacheCreationInputTokens == 0 {
		return
	}
	cliexec.Logf("[claude] [cache] op=%s in=%d out=%d cache_read=%d cache_write=%d 1h=%d 5m=%d cost_usd=%.4f",
		op,
		u.InputTokens,
		u.OutputTokens,
		u.CacheReadInputTokens,
		u.CacheCreationInputTokens,
		u.CacheCreation.Ephemeral1hInputTokens,
		u.CacheCreation.Ephemeral5mInputTokens,
		parsed.TotalCostUSD,
	)
}
