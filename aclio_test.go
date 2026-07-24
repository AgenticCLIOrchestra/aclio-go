package aclio

import (
	"errors"
	"fmt"
	"testing"

	"github.com/agenticcliorchestra/aclio-go/claude"
	"github.com/agenticcliorchestra/aclio-go/codex"
	"github.com/agenticcliorchestra/aclio-go/internal/cliexec"
)

func TestClaudeOptsMapping(t *testing.T) {
	opts := claudeOpts(Request{
		Provider:   Claude,
		Prompt:     "hi",
		Model:      "sonnet",
		JSONSchema: `{"type":"object"}`,
		ResumeID:   "sess-1",
		Name:       "op",
		Stream:     true,
		ClaudeOpts: &claude.RunOpts{
			AllowedTools:     []string{"Read"},
			SystemPromptFile: "/abs/sys.md",
			MaxTurns:         5,
			TempDir:          "/tmp/dbg",
			// These must NOT override the shared fields:
			Prompt:  "IGNORED",
			ModelID: "IGNORED",
		},
	})

	if opts.Prompt != "hi" || opts.ModelID != "sonnet" || opts.ResumeSessionID != "sess-1" {
		t.Errorf("shared fields not taken from Request: %+v", opts)
	}
	if !opts.Stream || opts.Name != "op" || opts.JsonSchema != `{"type":"object"}` {
		t.Errorf("shared fields wrong: %+v", opts)
	}
	if len(opts.AllowedTools) != 1 || opts.SystemPromptFile != "/abs/sys.md" || opts.MaxTurns != 5 || opts.TempDir != "/tmp/dbg" {
		t.Errorf("claude-only extras not overlaid: %+v", opts)
	}
}

func TestClaudeOptsDefaultModel(t *testing.T) {
	opts := claudeOpts(Request{Provider: Claude, Prompt: "hi"})
	if opts.ModelID != claude.Default {
		t.Errorf("empty model = %q, want %q", opts.ModelID, claude.Default)
	}
}

func TestCodexOptsMapping(t *testing.T) {
	opts := codexOpts(Request{
		Provider:   Codex,
		Prompt:     "hi",
		Model:      "gpt-5.5-codex",
		JSONSchema: `{"type":"object"}`,
		ResumeID:   "thread-1",
		Name:       "op",
		CodexOpts: &codex.RunOpts{
			Sandbox:          codex.SandboxWorkspaceWrite,
			SkipGitRepoCheck: true,
			Ephemeral:        true,
			TempDir:          "/tmp/dbg",
		},
	})

	if opts.Prompt != "hi" || opts.Model != "gpt-5.5-codex" || opts.ResumeSessionID != "thread-1" {
		t.Errorf("shared fields not taken from Request: %+v", opts)
	}
	if opts.OutputSchema != `{"type":"object"}` || opts.Name != "op" {
		t.Errorf("shared fields wrong: %+v", opts)
	}
	if opts.Sandbox != codex.SandboxWorkspaceWrite || !opts.SkipGitRepoCheck || !opts.Ephemeral || opts.TempDir != "/tmp/dbg" {
		t.Errorf("codex-only extras not overlaid: %+v", opts)
	}
}

func TestErrInterruptedReExport(t *testing.T) {
	// The public aliases must all be the one sentinel the guard returns, so a
	// consumer's errors.Is matches the wrapped error the runners produce.
	for name, e := range map[string]error{
		"aclio":  ErrInterrupted,
		"claude": claude.ErrInterrupted,
		"codex":  codex.ErrInterrupted,
	} {
		if !errors.Is(e, cliexec.ErrInterrupted) {
			t.Errorf("%s.ErrInterrupted is not the cliexec sentinel", name)
		}
	}
	// Matches across the wrap shape Capture/Stream use.
	wrapped := fmt.Errorf("%w: signal: killed", cliexec.ErrInterrupted)
	if !errors.Is(wrapped, ErrInterrupted) {
		t.Error("errors.Is does not match aclio.ErrInterrupted through a wrap")
	}
}

func TestRunUnknownProvider(t *testing.T) {
	if _, err := Run(Request{Provider: "gemini", Prompt: "hi"}); err == nil {
		t.Error("Run accepted an unknown provider")
	}
	if _, _, err := RunStructured[struct{}](Request{Provider: "gemini"}); err == nil {
		t.Error("RunStructured accepted an unknown provider")
	}
}
