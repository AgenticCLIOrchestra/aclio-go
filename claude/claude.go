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
	Prompt     string `json:"prompt"`
	JsonSchema string `json:"json_schema"`
	// Name is a short op label used in the [claude] [cache] log line emitted
	// after each call. Optional; defaults to "unnamed" when blank. Set per
	// call site so per-workflow cache hit rate is greppable.
	Name         string   `json:"name"`
	AllowedTools []string `json:"allowed_tools"`
	MaxTurns     int      `json:"max_turns"`
	ModelID      Model    `json:"model_id"`
	// SystemPromptFile is passed through to the CLI's --system-prompt-file.
	// Must be an absolute path — the CLI runs with absDir as its working dir,
	// so a relative path would resolve against that, not the caller's CWD.
	SystemPromptFile string `json:"system_prompt_file"`
	Stream           bool   `json:"stream"`
	ResumeSessionID  string `json:"resume_session_id"`
	// TempDir, when set, is a directory (created if needed) the call's
	// settings, prompt, output, and error are dumped into for debugging, as
	// {stamp}-{name}-{kind} with one millisecond-precise stamp per call, so
	// repeated calls never overwrite each other and ls lists them
	// chronologically. Excluded from the dumped settings JSON. Empty disables
	// the dumps.
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

	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
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

	return args, nil
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
