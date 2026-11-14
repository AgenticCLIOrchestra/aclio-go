package codex

import (
	"bytes"
	"strings"
	"testing"

	"github.com/agenticcliorchestra/aclio-go/internal/cliexec"
	"github.com/agenticcliorchestra/aclio-go/stream"
)

func TestHandleStreamLineRendering(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string // trimmed expected log output; "" means nothing logged
	}{
		{"thread started", `{"type":"thread.started","thread_id":"th-1"}`, "[codex] [thread] th-1"},
		{"agent message", `{"type":"item.completed","item":{"type":"agent_message","text":"  hi  "}}`, "[codex] [text] hi"},
		{"empty agent message", `{"type":"item.completed","item":{"type":"agent_message","text":"  "}}`, ""},
		{"reasoning", `{"type":"item.completed","item":{"type":"reasoning","text":"think"}}`, "[codex] [reasoning] think"},
		{"command", `{"type":"item.completed","item":{"type":"command_execution","command":"ls"}}`, "[codex] [command] ls"},
		{"other item", `{"type":"item.completed","item":{"type":"file_change"}}`, "[codex] [item] [file_change]"},
		{"malformed item", `{"type":"item.completed","item":"nope"}`, ""},
		{"turn completed", `{"type":"turn.completed","usage":{"input_tokens":1}}`, ""},
		{"error", `{"type":"error","message":"bad"}`, `[codex] [error] {"type":"error","message":"bad"}`},
		{"other type", `{"type":"turn.started"}`, "[codex] [turn.started]"},
		{"malformed json", `{nope`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := captureLog(t, "[codex]", func(l cliexec.Logger) {
				handleStreamLine(tc.line, &RunResult{}, true, l)
			})
			if strings.TrimSpace(got) != tc.want {
				t.Errorf("log = %q, want %q", strings.TrimSpace(got), tc.want)
			}
		})
	}
}

func TestHandleStreamLineRecordsResult(t *testing.T) {
	var result RunResult
	for _, line := range []string{
		`{"type":"thread.started","thread_id":"th-1"}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"first"}}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"final"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":7,"output_tokens":3}}`,
	} {
		// A handler that drops everything can't affect what the run returns.
		l := cliexec.NewLogger(&bytes.Buffer{}, "codex", "", func() stream.Handler {
			return func(stream.Event) []string { return nil }
		})
		handleStreamLine(line, &result, true, l)
	}
	if result.ThreadID != "th-1" || result.FinalText != "final" || result.Usage.InputTokens != 7 {
		t.Errorf("result = %+v", result)
	}
}

func TestHandlerEvents(t *testing.T) {
	var events []stream.Event
	l := cliexec.NewLogger(&bytes.Buffer{}, "codex", "w", func() stream.Handler {
		return func(e stream.Event) []string {
			events = append(events, e)
			return e.Lines
		}
	})
	for _, line := range []string{
		`{"type":"thread.started","thread_id":"th-1"}`,
		`{"type":"item.completed","item":{"type":"reasoning","text":"think"}}`,
		`{"type":"item.started","item":{"type":"command_execution"}}`,
		`{"type":"item.completed","item":"malformed"}`,
		`{"type":"turn.completed"}`,
	} {
		handleStreamLine(line, &RunResult{}, true, l)
	}

	type got struct{ typ, subtype string }
	want := []got{
		{"thread.started", ""},
		{"item.completed", "reasoning"},
		{"item.started", "command_execution"},
		{"turn.completed", ""},
	}
	if len(events) != len(want) {
		t.Fatalf("got %d events, want %d: %+v", len(events), len(want), events)
	}
	for i, w := range want {
		e := events[i]
		if e.Type != w.typ || e.Subtype != w.subtype || e.Kind != stream.KindStream || e.Provider != "codex" || e.Data != nil {
			t.Errorf("event %d = %+v, want type %q subtype %q", i, e, w.typ, w.subtype)
		}
	}
	if len(events[3].Lines) != 0 {
		t.Errorf("turn.completed Lines = %q, want none", events[3].Lines)
	}
}

// Without Stream, codex still parses every line, but nothing reaches the
// handler.
func TestHandlerNotCalledWithoutStream(t *testing.T) {
	calls := 0
	l := cliexec.NewLogger(&bytes.Buffer{}, "codex", "", func() stream.Handler {
		return func(e stream.Event) []string {
			calls++
			return e.Lines
		}
	})
	var result RunResult
	handleStreamLine(`{"type":"thread.started","thread_id":"th-1"}`, &result, false, l)
	if calls != 0 || result.ThreadID != "th-1" {
		t.Errorf("calls=%d threadID=%q", calls, result.ThreadID)
	}
}
