// Package codex drives the Codex CLI (`codex exec`) as a subprocess: live
// parsing of the CLI's JSONL event stream with opt-in [codex]-prefixed event
// and usage logging, structured output backed by a JSON schema, session
// resume, and (via internal/cliexec) a ctrl+c guard that keeps the terminal
// responsive while the CLI runs detached in its own session. Like the claude
// package, but against Codex's flags and event shapes rather than Claude
// Code's.
package codex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/agenticcliorchestra/aclio-go/internal/cliexec"
)

// ErrInterrupted is returned (wrapped) by Run when SIGINT/SIGTERM arrives while
// the CLI runs: the detached child is reaped and the call returns rather than
// exiting the process. Branch on it with errors.Is(err, codex.ErrInterrupted).
var ErrInterrupted = cliexec.ErrInterrupted

// SandboxMode selects Codex's sandbox policy for model-generated commands.
type SandboxMode string

const (
	SandboxReadOnly       SandboxMode = "read-only"
	SandboxWorkspaceWrite SandboxMode = "workspace-write"
	SandboxDangerFull     SandboxMode = "danger-full-access"
)

// modelRegex keeps the --model value from smuggling extra CLI flags. Codex
// accepts arbitrary provider model names (o3, gpt-5.5-codex, ...), so this is
// a shape check, not a whitelist.
var modelRegex = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// RunOpts configures one `codex exec` invocation.
type RunOpts struct {
	Prompt string `json:"prompt"`
	// Name is a short op label used in the [codex] [usage] log line emitted
	// after each call. Optional; defaults to "unnamed" when blank.
	Name string `json:"name"`
	// Model is the --model value; empty leaves Codex on its configured default.
	Model string `json:"model"`
	// OutputSchema is a JSON Schema (as a string) constraining the agent's
	// final message; it is written to a temp file for --output-schema.
	OutputSchema string `json:"output_schema"`
	// Sandbox maps to -s/--sandbox; empty leaves Codex on its configured default.
	Sandbox SandboxMode `json:"sandbox"`
	// ResumeSessionID continues a prior session (`codex exec resume <id>`).
	ResumeSessionID string `json:"resume_session_id"`
	// SkipGitRepoCheck maps to --skip-git-repo-check, needed to run outside a
	// git repository.
	SkipGitRepoCheck bool `json:"skip_git_repo_check"`
	// Ephemeral maps to --ephemeral (don't persist session files to disk).
	Ephemeral bool `json:"ephemeral"`
	// Stream, when true, logs each JSONL event live as a [codex] line. Events
	// are parsed either way; this only controls log verbosity.
	Stream bool `json:"stream"`
	// TempDir, when set, is a directory (created if needed) the call's
	// settings, prompt, output, and error are dumped into for debugging, as
	// {stamp}-{name}-{kind} with one millisecond-precise stamp per call, so
	// repeated calls never overwrite each other and ls lists them
	// chronologically. The output-schema file is written there too. Empty uses
	// an OS temp dir for the schema and disables dumps.
	TempDir string `json:"-"`
}

// Run invokes `codex exec` in absDir and returns the agent's final message
// text alongside a RunResult (thread/session id, token usage).
//
// If SIGINT/SIGTERM arrives while the CLI runs, Run reaps the detached child
// and returns an error wrapping ErrInterrupted rather than exiting the process;
// branch on it with errors.Is(err, codex.ErrInterrupted).
func Run(absDir string, opts RunOpts) (string, *RunResult, error) {
	stamp := cliexec.DebugStamp()

	schemaPath, cleanup, err := writeSchemaFile(opts, stamp)
	if err != nil {
		debugError(opts.TempDir, stamp, opts.Name, err)
		return "", nil, err
	}
	defer cleanup()

	args, err := buildArgs(opts, schemaPath)
	if err != nil {
		debugError(opts.TempDir, stamp, opts.Name, err)
		return "", nil, err
	}

	debugSettings(opts.TempDir, stamp, opts)
	debugPrompt(opts.TempDir, stamp, opts.Name, opts.Prompt)

	cmd := cliexec.Command(absDir, "codex", args)
	// The prompt travels via stdin, not as a positional argument: a single
	// argv element is capped at ~128 KB on Linux (MAX_ARG_STRLEN) and the
	// whole argv+env near ~1 MB elsewhere, and payload-carrying prompts blow
	// past that ("argument list too long"). The `-` positional (see buildArgs)
	// tells `codex exec` to read the prompt from stdin.
	cmd.Stdin = strings.NewReader(opts.Prompt)

	result, err := runStream(cmd, opts.Stream)
	if err != nil {
		debugError(opts.TempDir, stamp, opts.Name, err)
		return "", nil, err
	}

	logUsage(opts.Name, result)
	cliexec.DebugString(opts.TempDir, stamp+"-"+opts.Name+"-output.txt", result.FinalText)

	return result.FinalText, result, nil
}

// buildArgs assembles the `codex exec` arguments. The prompt is passed
// positionally (after a `--` guard so a prompt starting with `-` isn't parsed
// as a flag); resume routes through the `resume <id>` subcommand.
func buildArgs(opts RunOpts, schemaPath string) ([]string, error) {
	if opts.Model != "" && !modelRegex.MatchString(opts.Model) {
		return nil, errors.New("invalid model: " + opts.Model)
	}
	switch opts.Sandbox {
	case "", SandboxReadOnly, SandboxWorkspaceWrite, SandboxDangerFull:
	default:
		return nil, errors.New("invalid sandbox mode: " + string(opts.Sandbox))
	}

	args := []string{"exec", "--json"}
	if opts.ResumeSessionID != "" {
		args = append(args, "resume", opts.ResumeSessionID)
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.Sandbox != "" {
		args = append(args, "--sandbox", string(opts.Sandbox))
	}
	if opts.SkipGitRepoCheck {
		args = append(args, "--skip-git-repo-check")
	}
	if opts.Ephemeral {
		args = append(args, "--ephemeral")
	}
	if schemaPath != "" {
		args = append(args, "--output-schema", schemaPath)
	}
	// `-` as the positional prompt tells `codex exec` to read the prompt from
	// stdin (fed by Run); the `--` guard keeps it unambiguously positional.
	args = append(args, "--", "-")
	return args, nil
}

// writeSchemaFile materialises opts.OutputSchema to a file for --output-schema
// (Codex takes a path, not an inline schema), stamp-prefixed like the debug
// dumps so repeated calls into the same TempDir don't overwrite each other.
// Returns an empty path and a no-op cleanup when no schema is set.
func writeSchemaFile(opts RunOpts, stamp string) (path string, cleanup func(), err error) {
	if opts.OutputSchema == "" {
		return "", func() {}, nil
	}
	dir := opts.TempDir
	ephemeral := false
	if dir == "" {
		dir, err = os.MkdirTemp("", "codex-schema-*")
		if err != nil {
			return "", func() {}, fmt.Errorf("creating schema temp dir: %w", err)
		}
		ephemeral = true
	} else if err := os.MkdirAll(dir, 0755); err != nil {
		return "", func() {}, fmt.Errorf("creating schema dir: %w", err)
	}
	path = filepath.Join(dir, stamp+"-"+cliexec.DebugName(opts.Name)+"-schema.json")
	if err := os.WriteFile(path, []byte(opts.OutputSchema), 0644); err != nil {
		if ephemeral {
			_ = os.RemoveAll(dir)
		}
		return "", func() {}, fmt.Errorf("writing output schema: %w", err)
	}
	if ephemeral {
		return path, func() { _ = os.RemoveAll(dir) }, nil
	}
	return path, func() {}, nil
}

// logUsage writes one greppable [codex] [usage] line per call.
func logUsage(name string, r *RunResult) {
	if r == nil {
		return
	}
	op := name
	if op == "" {
		op = "unnamed"
	}
	u := r.Usage
	if u.InputTokens == 0 && u.OutputTokens == 0 && u.CachedInputTokens == 0 {
		return
	}
	cliexec.Logf("[codex] [usage] op=%s in=%d out=%d cached=%d reasoning=%d",
		op, u.InputTokens, u.OutputTokens, u.CachedInputTokens, u.ReasoningOutputTokens)
}
