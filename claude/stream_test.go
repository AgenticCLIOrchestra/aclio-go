package claude

import (
	"bytes"
	"strings"
	"testing"

	"github.com/agenticcliorchestra/aclio-go/internal/cliexec"
)

// captureLog runs fn with a logger that writes to a fresh buffer under
// prefix, and returns what was written. Each call gets its own writer, so
// tests never touch the process-wide sink.
func captureLog(t *testing.T, prefix string, fn func(cliexec.Logger)) string {
	t.Helper()
	var buf bytes.Buffer
	fn(cliexec.Logger{Writer: &buf, Prefix: prefix})
	return buf.String()
}

func TestHandleStreamLineRendering(t *testing.T) {
	big := strings.Repeat("x", 250)
	bigDenied := strings.Repeat("y", 250)
	sysOther := `{"type":"system","subtype":"rate_limit_event"}`
	sysThinking := `{"type":"system","subtype":"thinking_tokens","estimated_tokens":300,"estimated_tokens_delta":100}`
	sysBadInit := `{"type":"system","subtype":"init","tools":"not a list"}`

	cases := []struct {
		name string
		line string
		want string // trimmed expected log output; "" means nothing logged
	}{
		{
			"thinking trimmed",
			`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"  plan the work  "}]}}`,
			"[claude] [thinking] plan the work",
		},
		{
			"text trimmed",
			`{"type":"assistant","message":{"content":[{"type":"text","text":"hello there"}]}}`,
			"[claude] [text] hello there",
		},
		{
			"thinking truncated at 200",
			`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"` + big + `"}]}}`,
			"[claude] [thinking] " + strings.Repeat("x", 200) + "...",
		},
		{
			"tool_use Bash summary",
			`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}`,
			"[claude] [tool] [Bash] go test ./...",
		},
		{
			"tool_use Read summary",
			`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/a/b.go"}}]}}`,
			"[claude] [tool] [Read] /a/b.go",
		},
		{
			"tool_use Edit summary",
			`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/x/y.go"}}]}}`,
			"[claude] [tool] [Edit] /x/y.go",
		},
		{
			"tool_use Write summary",
			`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":"/z.txt"}}]}}`,
			"[claude] [tool] [Write] /z.txt",
		},
		{
			"tool_use Grep with path",
			`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Grep","input":{"pattern":"foo","path":"/x"}}]}}`,
			"[claude] [tool] [Grep] foo in /x",
		},
		{
			"tool_use Grep without path",
			`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Grep","input":{"pattern":"TODO"}}]}}`,
			"[claude] [tool] [Grep] TODO",
		},
		{
			"tool_use Glob summary",
			`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Glob","input":{"pattern":"**/*.go"}}]}}`,
			"[claude] [tool] [Glob] **/*.go",
		},
		{
			"tool_use unknown falls back to raw JSON",
			`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Frobnicate","input":{"a":1}}]}}`,
			`[claude] [tool] [Frobnicate] {"a":1}`,
		},
		{
			"denied tool_result trimmed",
			`{"type":"user","message":{"content":[{"type":"tool_result","is_error":true,"content":"  nope, blocked  "}]}}`,
			"[claude] [tool] [denied] nope, blocked",
		},
		{
			"denied tool_result truncated at 200",
			`{"type":"user","message":{"content":[{"type":"tool_result","is_error":true,"content":"` + bigDenied + `"}]}}`,
			"[claude] [tool] [denied] " + strings.Repeat("y", 200) + "...",
		},
		{
			"tool_result without error is silent",
			`{"type":"user","message":{"content":[{"type":"tool_result","is_error":false,"content":"ok"}]}}`,
			"",
		},
		{
			"system init collapses to marker",
			`{"type":"system","subtype":"init"}`,
			"[claude] [system] init",
		},
		{
			"system other subtype passes through raw",
			sysOther,
			"[claude] [system] " + sysOther,
		},
		{
			"system known subtype passes through raw",
			sysThinking,
			"[claude] [system] " + sysThinking,
		},
		{
			"system known subtype with mismatched fields passes through raw",
			sysBadInit,
			"[claude] [system] " + sysBadInit,
		},
		{
			"unknown type without subtype",
			`{"type":"mystery"}`,
			"[claude] [mystery]",
		},
		{
			"unknown type with subtype",
			`{"type":"mystery","subtype":"weird"}`,
			"[claude] [mystery] [weird]",
		},
		{
			"malformed json is skipped",
			`{not json`,
			"",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var lastResult string
			out := captureLog(t, "[claude]", func(l cliexec.Logger) { handleStreamLine(tc.line, &lastResult, l) })
			if got := strings.TrimSpace(out); got != tc.want {
				t.Errorf("log = %q\nwant  %q", got, tc.want)
			}
			if lastResult != "" {
				t.Errorf("non-result line set lastResult to %q", lastResult)
			}
		})
	}
}

func TestHandleStreamLineCapturesResult(t *testing.T) {
	line := `{"type":"result","session_id":"s1","result":"done"}`
	var lastResult string
	out := captureLog(t, "[claude]", func(l cliexec.Logger) { handleStreamLine(line, &lastResult, l) })

	if strings.TrimSpace(out) != "" {
		t.Errorf("a result line should not be logged, got %q", out)
	}
	if lastResult != line {
		t.Errorf("lastResult = %q, want the raw result line", lastResult)
	}
}

// TestHandleStreamLineNamedPrefix — when the run is named, every line is led by
// "[claude] [name]" so a named run's output is filterable by name.
func TestHandleStreamLineNamedPrefix(t *testing.T) {
	var lastResult string
	out := captureLog(t, "[claude] [triage]", func(l cliexec.Logger) {
		handleStreamLine(
			`{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`,
			&lastResult, l)
	})
	if got := strings.TrimSpace(out); got != "[claude] [triage] [text] hi" {
		t.Errorf("log = %q, want the name-prefixed line", got)
	}
}
