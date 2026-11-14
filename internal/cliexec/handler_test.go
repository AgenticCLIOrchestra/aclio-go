package cliexec

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/agenticcliorchestra/aclio-go/stream"
)

// recorder returns a factory whose handlers append every event they see to
// *events and reply with reply(e).
func recorder(events *[]stream.Event, reply func(stream.Event) []string) stream.HandlerFactory {
	return func() stream.Handler {
		return func(e stream.Event) []string {
			*events = append(*events, e)
			return reply(e)
		}
	}
}

func keep(e stream.Event) []string { return e.Lines }

func TestEmitWithoutHandlerWritesLines(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf, "claude", "", nil)
	Emit(l, stream.Event{Kind: stream.KindStart}, []string{"[claude] a", "[claude] b"})
	EmitStream(l, "system", "init", `{"type":"system"}`, nil, []string{"[claude] c"})
	End(l)
	if got, want := buf.String(), "[claude] a\n[claude] b\n[claude] c\n"; got != want {
		t.Errorf("wrote %q, want %q", got, want)
	}
}

func TestEmitFillsEventAndWritesHandlerLines(t *testing.T) {
	var buf bytes.Buffer
	var events []stream.Event
	l := NewLogger(&buf, "codex", "worker", recorder(&events, func(e stream.Event) []string {
		if e.Kind == stream.KindEnd {
			return []string{"bye"}
		}
		return append([]string{"before"}, e.Lines...)
	}))

	raw := `{"type":"item.completed"}`
	EmitStream(l, "item.completed", "reasoning", raw, "data", []string{"[codex] [worker] x"})
	End(l)

	if got, want := buf.String(), "before\n[codex] [worker] x\nbye\n"; got != want {
		t.Errorf("wrote %q, want %q", got, want)
	}
	want := []stream.Event{
		{
			Kind: stream.KindStream, Provider: "codex", Name: "worker", Prefix: "[codex] [worker]",
			Type: "item.completed", Subtype: "reasoning", Raw: []byte(raw), Data: "data",
			Lines: []string{"[codex] [worker] x"},
		},
		{Kind: stream.KindEnd, Provider: "codex", Name: "worker", Prefix: "[codex] [worker]"},
	}
	if !reflect.DeepEqual(events, want) {
		t.Errorf("events =\n %#v\nwant\n %#v", events, want)
	}
}

func TestHandlerReturningNilWritesNothing(t *testing.T) {
	var buf bytes.Buffer
	var events []stream.Event
	l := NewLogger(&buf, "claude", "", recorder(&events, func(stream.Event) []string { return nil }))
	Emit(l, stream.Event{Kind: stream.KindStart}, []string{"[claude] [start]"})
	if buf.Len() != 0 {
		t.Errorf("wrote %q, want nothing", buf.String())
	}
}

func TestNewLoggerCallsFactoryOncePerRun(t *testing.T) {
	calls := 0
	factory := func() stream.Handler {
		calls++
		return keep
	}
	var buf bytes.Buffer
	l := NewLogger(&buf, "claude", "", factory)
	Emit(l, stream.Event{Kind: stream.KindStart}, []string{"a"})
	Emit(l, stream.Event{Kind: stream.KindUsage}, []string{"b"})
	if calls != 1 {
		t.Errorf("factory called %d times for one run, want 1", calls)
	}
	NewLogger(&buf, "claude", "", factory)
	if calls != 2 {
		t.Errorf("factory called %d times for two runs, want 2", calls)
	}
}

func TestNewLoggerFactoryReturningNil(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf, "claude", "", func() stream.Handler { return nil })
	if l.Handler != nil {
		t.Fatal("a nil handler from the factory should leave the logger without one")
	}
	Emit(l, stream.Event{Kind: stream.KindStart}, []string{"a"})
	if buf.String() != "a\n" {
		t.Errorf("wrote %q, want the lines as they are", buf.String())
	}
}

func TestNewLoggerHandlerPrecedence(t *testing.T) {
	t.Cleanup(func() { SetEventHandlerFactory(nil) })
	var global, own []stream.Event
	SetEventHandlerFactory(recorder(&global, keep))

	var buf bytes.Buffer
	Emit(NewLogger(&buf, "claude", "", recorder(&own, keep)), stream.Event{Kind: stream.KindStart}, nil)
	if len(own) != 1 || len(global) != 0 {
		t.Errorf("own=%d global=%d; a per-call handler should replace the process-wide one", len(own), len(global))
	}

	Emit(NewLogger(&buf, "claude", "", nil), stream.Event{Kind: stream.KindStart}, nil)
	if len(global) != 1 {
		t.Errorf("global=%d; without a per-call handler the process-wide one should be used", len(global))
	}
}

// A run keeps the handler it started with even if the process-wide one
// changes mid-run, and clearing it affects only later runs.
func TestNewLoggerSnapshotsDefaultHandler(t *testing.T) {
	t.Cleanup(func() { SetEventHandlerFactory(nil) })
	var first, second []stream.Event
	SetEventHandlerFactory(recorder(&first, keep))
	var buf bytes.Buffer
	l := NewLogger(&buf, "claude", "", nil)
	SetEventHandlerFactory(recorder(&second, keep))

	Emit(l, stream.Event{Kind: stream.KindStart}, nil)
	if len(first) != 1 || len(second) != 0 {
		t.Errorf("first=%d second=%d; the run should keep its starting handler", len(first), len(second))
	}

	SetEventHandlerFactory(nil)
	if DefaultEventHandlerFactory() != nil {
		t.Error("SetEventHandlerFactory(nil) should clear the process-wide handler")
	}
	if NewLogger(&buf, "claude", "", nil).Handler != nil {
		t.Error("a run started after clearing should have no handler")
	}
}

// Raw is the handler's own copy: changing it doesn't touch anything aclio-go
// keeps, and it isn't built at all without a handler.
func TestEmitStreamRawIsACopy(t *testing.T) {
	var events []stream.Event
	var buf bytes.Buffer
	l := NewLogger(&buf, "claude", "", recorder(&events, keep))
	raw := `{"type":"system"}`
	EmitStream(l, "system", "", raw, nil, nil)
	events[0].Raw[0] = 'X'
	if raw != `{"type":"system"}` {
		t.Errorf("raw changed to %q", raw)
	}
}
