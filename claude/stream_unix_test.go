//go:build unix

package claude

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agenticcliorchestra/aclio-go/internal/cliexec"
)

// These tests drive runStream through a real process (`cat` replaying a JSONL
// fixture), so they're unix-gated — `cat` isn't on a stock windows runner. The
// portable rendering contract is pinned in stream_test.go via handleStreamLine.

// discard is a logger for tests that only assert on return values.
var discard = cliexec.Logger{Writer: io.Discard, Prefix: "[claude]"}

// writeStream writes JSONL lines to a temp file and returns a `cat` command that
// replays them through runStream — a process-backed stand-in for the CLI.
func writeStream(t *testing.T, lines ...string) *exec.Cmd {
	t.Helper()
	f := filepath.Join(t.TempDir(), "stream.jsonl")
	if err := os.WriteFile(f, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return exec.Command("cat", f)
}

func TestRunStreamCapturesResult(t *testing.T) {
	cmd := writeStream(t,
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"result","session_id":"s1","result":"done"}`,
	)
	res, err := runStream(cmd, discard)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, `"session_id":"s1"`) {
		t.Errorf("result blob = %q, want the result line", res)
	}
}

func TestRunStreamMissingResult(t *testing.T) {
	cmd := writeStream(t,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"no result follows"}]}}`,
	)
	if _, err := runStream(cmd, discard); err == nil {
		t.Error("runStream should error when the stream has no result event")
	}
}

func TestRunStreamLargeLine(t *testing.T) {
	// ~1.5 MB single line: exceeds the scanner's 1 MB initial buffer, well under
	// its 10 MB max — guards cliexec.Stream's buffer sizing.
	big := strings.Repeat("x", 1_500_000)
	cmd := writeStream(t, `{"type":"result","session_id":"s","result":"`+big+`"}`)
	res, err := runStream(cmd, discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) < 1_000_000 {
		t.Errorf("large result blob was truncated: len = %d", len(res))
	}
}
