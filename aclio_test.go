package aclio

import (
	"bytes"
	"errors"
	"fmt"
	"os"
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
			ForkSession:      true,
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
	if len(opts.AllowedTools) != 1 || opts.SystemPromptFile != "/abs/sys.md" || opts.MaxTurns != 5 || opts.TempDir != "/tmp/dbg" || !opts.ForkSession {
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

// The per-call LogWriter is a shared field: it always comes from the Request,
// never from the provider escape hatch.
func TestLogWriterMapping(t *testing.T) {
	var reqW, extraW bytes.Buffer

	c := claudeOpts(Request{Provider: Claude, LogWriter: &reqW, ClaudeOpts: &claude.RunOpts{LogWriter: &extraW}})
	if c.LogWriter != &reqW {
		t.Error("claude: LogWriter not taken from Request")
	}
	x := codexOpts(Request{Provider: Codex, LogWriter: &reqW, CodexOpts: &codex.RunOpts{LogWriter: &extraW}})
	if x.LogWriter != &reqW {
		t.Error("codex: LogWriter not taken from Request")
	}

	if claudeOpts(Request{Provider: Claude}).LogWriter != nil || codexOpts(Request{Provider: Codex}).LogWriter != nil {
		t.Error("an unset Request.LogWriter must stay nil so the process-wide sink is used")
	}
}

// Stderr is a shared field too: it always comes from the Request.
func TestStderrMapping(t *testing.T) {
	var reqW, extraW bytes.Buffer

	c := claudeOpts(Request{Provider: Claude, Stderr: &reqW, ClaudeOpts: &claude.RunOpts{Stderr: &extraW}})
	if c.Stderr != &reqW {
		t.Error("claude: Stderr not taken from Request")
	}
	x := codexOpts(Request{Provider: Codex, Stderr: &reqW, CodexOpts: &codex.RunOpts{Stderr: &extraW}})
	if x.Stderr != &reqW {
		t.Error("codex: Stderr not taken from Request")
	}

	if claudeOpts(Request{Provider: Claude}).Stderr != nil || codexOpts(Request{Provider: Codex}).Stderr != nil {
		t.Error("an unset Request.Stderr must stay nil so os.Stderr is used")
	}
}

func TestSetLogWriter(t *testing.T) {
	t.Cleanup(func() { SetLogWriter(nil) })

	var buf bytes.Buffer
	SetLogWriter(&buf)
	if cliexec.DefaultLogWriter() != &buf {
		t.Error("SetLogWriter did not set the process-wide sink")
	}
	SetLogWriter(nil)
	if cliexec.DefaultLogWriter() != os.Stderr {
		t.Error("SetLogWriter(nil) should restore os.Stderr")
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
