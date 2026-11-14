//go:build unix

package claude

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/agenticcliorchestra/aclio-go/stream"
)

// fakeCLI puts an executable named name first on PATH that discards its stdin
// and prints lines, standing in for the real CLI.
func fakeCLI(t *testing.T, name string, lines ...string) {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "out.jsonl")
	if err := os.WriteFile(out, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncat >/dev/null\ncat '" + out + "'\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A run's events arrive start, stream..., usage, end, and End comes last
// even though it is deferred.
func TestRunEventOrder(t *testing.T) {
	fakeCLI(t, "claude",
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"result","session_id":"s1","result":"done","total_cost_usd":0.01,"usage":{"input_tokens":10,"output_tokens":5}}`,
	)
	var kinds []string
	var buf bytes.Buffer
	_, err := Run(t.TempDir(), RunOpts{
		Prompt:    "hi",
		ModelID:   Default,
		Stream:    true,
		LogWriter: &buf,
		EventHandlerFactory: func() stream.Handler {
			return func(e stream.Event) []string {
				k := string(e.Kind)
				if e.Kind == stream.KindStream {
					k += ":" + e.Type
				}
				kinds = append(kinds, k)
				if e.Kind == stream.KindEnd {
					return []string{"end"}
				}
				return e.Lines
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"start", "stream:system", "stream:assistant", "stream:result", "usage", "end"}
	if !reflect.DeepEqual(kinds, want) {
		t.Errorf("kinds = %q, want %q", kinds, want)
	}
	if !strings.HasSuffix(buf.String(), "\nend\n") || !strings.Contains(buf.String(), "[claude] [cache] op=unnamed") {
		t.Errorf("log = %q", buf.String())
	}
}

// Without streaming there are no stream events, but start, usage and end
// still go through the handler.
func TestRunEventsWithoutStream(t *testing.T) {
	fakeCLI(t, "claude",
		`{"type":"result","session_id":"s1","result":"done","usage":{"input_tokens":10,"output_tokens":5}}`,
	)
	var kinds []stream.Kind
	_, err := Run(t.TempDir(), RunOpts{
		Prompt:    "hi",
		ModelID:   Default,
		LogWriter: &bytes.Buffer{},
		EventHandlerFactory: func() stream.Handler {
			return func(e stream.Event) []string {
				kinds = append(kinds, e.Kind)
				return e.Lines
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []stream.Kind{stream.KindStart, stream.KindUsage, stream.KindEnd}; !reflect.DeepEqual(kinds, want) {
		t.Errorf("kinds = %q, want %q", kinds, want)
	}
}
