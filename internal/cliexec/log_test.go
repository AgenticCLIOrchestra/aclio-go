package cliexec

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestLog(t *testing.T) {
	var buf bytes.Buffer
	Log(Logger{Writer: &buf, Prefix: "[claude] [triage]"}, "[start] op=%s n=%d", "triage", 5)
	if got, want := buf.String(), "[claude] [triage] [start] op=triage n=5\n"; got != want {
		t.Errorf("Log wrote %q, want %q", got, want)
	}
}

// A run name is caller input: a % in it must come out verbatim, not be
// treated as a format verb.
func TestLogPrefixIsNotAFormat(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf, "codex", "100%s done", nil)
	Log(l, "[text] %s", "hi")
	if got, want := buf.String(), "[codex] [100%s done] [text] hi\n"; got != want {
		t.Errorf("Log wrote %q, want %q", got, want)
	}
}

func TestNewLoggerUsesOwnWriter(t *testing.T) {
	t.Cleanup(func() { SetLogWriter(nil) })
	var global, own bytes.Buffer
	SetLogWriter(&global)

	Log(NewLogger(&own, "claude", "", nil), "[x]")
	Log(NewLogger(nil, "claude", "", nil), "[y]")

	if own.String() != "[claude] [x]\n" {
		t.Errorf("own writer got %q", own.String())
	}
	if global.String() != "[claude] [y]\n" {
		t.Errorf("process-wide sink got %q", global.String())
	}
}

// A run keeps the sink it started with even if the process-wide one changes
// mid-run.
func TestNewLoggerSnapshotsDefault(t *testing.T) {
	t.Cleanup(func() { SetLogWriter(nil) })
	var first, second bytes.Buffer
	SetLogWriter(&first)
	l := NewLogger(nil, "claude", "", nil)
	SetLogWriter(&second)

	Log(l, "[x]")
	if first.String() != "[claude] [x]\n" || second.Len() != 0 {
		t.Errorf("first=%q second=%q; the run should keep its starting sink", first.String(), second.String())
	}
}

func TestSetLogWriterNilRestoresStderr(t *testing.T) {
	t.Cleanup(func() { SetLogWriter(nil) })
	SetLogWriter(io.Discard)
	SetLogWriter(nil)
	if DefaultLogWriter() != os.Stderr {
		t.Error("SetLogWriter(nil) should restore os.Stderr")
	}
}

// syncBuffer is a bytes.Buffer safe for concurrent writers.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// Concurrent runs logging to their own writers while the process-wide sink is
// swapped must be race-free (run with -race), and each run's lines must land
// only in its own writer.
func TestConcurrentLoggersAndSetLogWriter(t *testing.T) {
	t.Cleanup(func() { SetLogWriter(nil) })
	const runs, lines = 8, 200

	bufs := make([]*syncBuffer, runs)
	var wg sync.WaitGroup
	for i := range runs {
		bufs[i] = &syncBuffer{}
		wg.Go(func() {
			l := NewLogger(bufs[i], "claude", string(rune('a'+i)), nil)
			for range lines {
				Log(l, "[text] hi")
			}
		})
	}
	wg.Go(func() {
		for range lines {
			SetLogWriter(io.Discard)
			Log(NewLogger(nil, "codex", "", nil), "[x]")
			SetLogWriter(nil)
		}
	})
	wg.Wait()

	for i, b := range bufs {
		want := "[claude] [" + string(rune('a'+i)) + "] [text] hi\n"
		got := b.buf.String()
		if strings.Count(got, want) != lines || len(got) != lines*len(want) {
			t.Errorf("run %d: got %d bytes, want %d copies of %q", i, len(got), lines, want)
		}
	}
}
