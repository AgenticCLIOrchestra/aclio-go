package claude

import (
	"bytes"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/agenticcliorchestra/aclio-go/internal/cliexec"
	"github.com/agenticcliorchestra/aclio-go/stream"
	"github.com/agenticcliorchestra/aclio-go/stream/claudeevent"
)

// handled runs lines through handleStreamLine with a handler that records
// every event and replies with reply(e), returning the events, what was
// written and the captured result.
func handled(t *testing.T, reply func(stream.Event) []string, lines ...string) ([]stream.Event, string, string) {
	t.Helper()
	var events []stream.Event
	var buf bytes.Buffer
	l := cliexec.NewLogger(&buf, "claude", "worker", func() stream.Handler {
		return func(e stream.Event) []string {
			events = append(events, e)
			return reply(e)
		}
	})
	var lastResult string
	for _, line := range lines {
		handleStreamLine(line, &lastResult, l)
	}
	return events, buf.String(), lastResult
}

func keepLines(e stream.Event) []string { return e.Lines }

func TestHandlerGetsTypedData(t *testing.T) {
	assistant := `{"type":"assistant","message":{"content":[{"type":"text","text":"hi"},{"type":"thinking","thinking":"hmm"}]}}`
	user := `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"no","is_error":true}]}}`
	thinking := `{"type":"system","subtype":"thinking_tokens","estimated_tokens":300,"estimated_tokens_delta":100}`
	unknown := `{"type":"system","subtype":"brand_new"}`
	result := `{"type":"result","result":"done"}`

	events, out, lastResult := handled(t, keepLines, assistant, user, thinking, unknown, result)

	if len(events) != 5 {
		t.Fatalf("got %d events, want 5", len(events))
	}
	if d, ok := events[0].Data.(claudeevent.AssistantEvent); !ok || len(d.Message.Content) != 2 {
		t.Errorf("assistant Data = %#v", events[0].Data)
	}
	if want := []string{"[claude] [worker] [text] hi", "[claude] [worker] [thinking] hmm"}; !reflect.DeepEqual(events[0].Lines, want) {
		t.Errorf("assistant Lines = %q, want %q", events[0].Lines, want)
	}
	if d, ok := events[1].Data.(claudeevent.UserEvent); !ok || d.Message.Content[0].ToolUseID != "t1" {
		t.Errorf("user Data = %#v", events[1].Data)
	}
	if d, ok := events[2].Data.(claudeevent.ThinkingTokensEvent); !ok || d.EstimatedTokens != 300 {
		t.Errorf("thinking Data = %#v", events[2].Data)
	}
	if events[2].Type != "system" || events[2].Subtype != "thinking_tokens" || string(events[2].Raw) != thinking {
		t.Errorf("thinking event = %+v", events[2])
	}
	if d, ok := events[3].Data.(claudeevent.UnknownEvent); !ok || d.Subtype != "brand_new" {
		t.Errorf("unknown system Data = %#v", events[3].Data)
	}
	if events[4].Data != nil || len(events[4].Lines) != 0 {
		t.Errorf("result event = %+v, want no Data and no Lines", events[4])
	}
	for _, e := range events {
		if e.Kind != stream.KindStream || e.Provider != "claude" || e.Name != "worker" || e.Prefix != "[claude] [worker]" {
			t.Errorf("event header = %+v", e)
		}
	}

	// Returning e.Lines keeps the default output exactly.
	want := "[claude] [worker] [text] hi\n[claude] [worker] [thinking] hmm\n" +
		"[claude] [worker] [tool] [denied] no\n" +
		"[claude] [worker] [system] " + thinking + "\n" +
		"[claude] [worker] [system] " + unknown + "\n"
	if out != want {
		t.Errorf("wrote\n%q\nwant\n%q", out, want)
	}
	if lastResult != result {
		t.Errorf("lastResult = %q", lastResult)
	}
}

// The result is captured before the handler runs, so dropping the line can't
// lose it.
func TestHandlerCannotDropResult(t *testing.T) {
	result := `{"type":"result","result":"done"}`
	_, out, lastResult := handled(t, func(stream.Event) []string { return nil }, result)
	if lastResult != result || out != "" {
		t.Errorf("lastResult=%q out=%q", lastResult, out)
	}
}

func TestHandlerNeverSeesMalformedLines(t *testing.T) {
	events, _, _ := handled(t, keepLines, `{not json`, ``)
	if len(events) != 0 {
		t.Errorf("got %d events for malformed lines, want 0", len(events))
	}
}

// A line whose fields don't match its type still reaches the handler, with
// nil Data, so nothing is hidden from it.
func TestHandlerSeesMismatchedLines(t *testing.T) {
	events, out, _ := handled(t, keepLines,
		`{"type":"assistant","message":"not an object"}`,
		`{"type":"system","subtype":"init","tools":"not a list"}`,
	)
	if len(events) != 2 || events[0].Data != nil || events[1].Data != nil {
		t.Fatalf("events = %+v", events)
	}
	if len(events[0].Lines) != 0 {
		t.Errorf("mismatched assistant Lines = %q, want none", events[0].Lines)
	}
	if !strings.Contains(out, `[system] {"type":"system","subtype":"init","tools":"not a list"}`) {
		t.Errorf("mismatched system line not logged raw: %q", out)
	}
}

// The squash case from the feature request: keep only the latest thinking
// tick of a run of them, written just before the next other event.
func TestHandlerSquashesThinkingTokens(t *testing.T) {
	tick := func(n string) string {
		return `{"type":"system","subtype":"thinking_tokens","estimated_tokens":` + n + `}`
	}
	var held []string
	squash := func(e stream.Event) []string {
		if _, ok := e.Data.(claudeevent.ThinkingTokensEvent); ok {
			held = e.Lines
			return nil
		}
		out := slices.Concat(held, e.Lines)
		held = nil
		return out
	}
	_, out, _ := handled(t, squash,
		tick("100"), tick("200"), tick("300"),
		`{"type":"assistant","message":{"content":[{"type":"text","text":"done"}]}}`,
	)
	want := "[claude] [worker] [system] " + tick("300") + "\n[claude] [worker] [text] done\n"
	if out != want {
		t.Errorf("wrote\n%q\nwant\n%q", out, want)
	}
}
