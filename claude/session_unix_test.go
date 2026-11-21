//go:build unix

package claude

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeSessionCLI puts a `claude` first on PATH that speaks a sliver of the
// stream-json protocol: each user message gets an init, an assistant line and
// a result whose structured_output counts the turns and whose total cost
// grows by 0.01 a turn; an interrupt gets a control_response and an aborted
// result; a message containing "die" makes it exit; one containing "ask" makes
// it send its own control_request first and wait for the reply. Its argv is
// written to args.txt in the returned dir.
func fakeSessionCLI(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
printf '%s\n' "$@" > '` + dir + `/args.txt'
n=0
while IFS= read -r line; do
  case "$line" in
    *'"control_response"'*) continue ;;
    *'"subtype":"interrupt"'*)
      id=$(printf '%s' "$line" | sed 's/.*"request_id":"\([^"]*\)".*/\1/')
      printf '{"type":"control_response","response":{"subtype":"success","request_id":"%s","response":{}}}\n' "$id"
      printf '{"type":"result","subtype":"error_during_execution","is_error":true,"terminal_reason":"aborted_streaming","session_id":"s1","total_cost_usd":0.025}\n'
      continue ;;
    *'"type":"user"'*)
      case "$line" in *die*) exit 3 ;; esac
      case "$line" in *hang*) continue ;; esac
      case "$line" in *ask*)
        printf '{"type":"control_request","request_id":"cli_1","request":{"subtype":"can_use_tool"}}\n'
        IFS= read -r reply
        case "$reply" in *'"request_id":"cli_1"'*) ;; *) exit 4 ;; esac ;;
      esac
      n=$((n+1))
      printf '{"type":"system","subtype":"init"}\n'
      printf '{"type":"assistant","message":{"content":[{"type":"text","text":"turn %d"}]}}\n' "$n"
      printf '{"type":"result","subtype":"success","session_id":"s1","total_cost_usd":0.0%d,"usage":{"input_tokens":10,"output_tokens":5},"structured_output":{"n":%d}}\n' "$n" "$n" ;;
  esac
done
`
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

type counted struct {
	N int `json:"n"`
}

func startFake(t *testing.T, log *bytes.Buffer) *Session {
	t.Helper()
	s, err := Start(t.TempDir(), RunOpts{ModelID: Default, Name: "sess", LogWriter: log, JsonSchema: `{"type":"object"}`})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close(s) })
	return s
}

// Turns share one process: the counter in structured_output keeps climbing,
// and each turn reports its own share of the CLI's running cost.
func TestSessionTurnsShareOneProcess(t *testing.T) {
	dir := fakeSessionCLI(t)
	var log bytes.Buffer
	s := startFake(t, &log)

	for want := 1; want <= 3; want++ {
		v, parsed, err := SendStructured[counted](s, "go")
		if err != nil {
			t.Fatalf("turn %d: %v", want, err)
		}
		if v.N != want {
			t.Errorf("turn %d: n = %d", want, v.N)
		}
		if diff := parsed.TurnCostUSD - 0.01; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("turn %d: TurnCostUSD = %v, want 0.01", want, parsed.TurnCostUSD)
		}
	}
	if got := SessionID(s); got != "s1" {
		t.Errorf("SessionID = %q", got)
	}
	if c := strings.Count(log.String(), "[claude] [sess] [start]"); c != 3 {
		t.Errorf("%d [start] lines, want one per turn:\n%s", c, log.String())
	}
	if !strings.Contains(log.String(), "cost_usd=0.0100") {
		t.Errorf("cache line lacks the turn cost:\n%s", log.String())
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args.txt"))
	if !strings.Contains(string(args), "--input-format\nstream-json") || !strings.Contains(string(args), "--output-format\nstream-json") {
		t.Errorf("args = %q", args)
	}
}

// A control_request from the CLI is refused rather than left hanging.
func TestSessionRefusesCLIControlRequests(t *testing.T) {
	fakeSessionCLI(t)
	s := startFake(t, &bytes.Buffer{})
	v, _, err := SendStructured[counted](s, "ask")
	if err != nil || v.N != 1 {
		t.Fatalf("v = %+v, err = %v", v, err)
	}
}

// Interrupt stops the turn in flight; the session stays usable.
func TestSessionInterrupt(t *testing.T) {
	fakeSessionCLI(t)
	s := startFake(t, &bytes.Buffer{})

	done := make(chan error, 1)
	go func() {
		_, err := Send(s, "hang")
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	if err := Interrupt(s); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrInterrupted) {
			t.Fatalf("err = %v, want ErrInterrupted", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("interrupted Send did not return")
	}

	v, _, err := SendStructured[counted](s, "go")
	if err != nil || v.N != 1 {
		t.Fatalf("after interrupt: v = %+v, err = %v", v, err)
	}
}

// A dead CLI ends the session.
func TestSessionEndsWhenCLIDies(t *testing.T) {
	fakeSessionCLI(t)
	s := startFake(t, &bytes.Buffer{})
	if _, err := Send(s, "die"); !errors.Is(err, ErrSessionEnded) {
		t.Fatalf("err = %v, want ErrSessionEnded", err)
	}
	if _, err := Send(s, "go"); !errors.Is(err, ErrSessionEnded) {
		t.Fatalf("second err = %v, want ErrSessionEnded", err)
	}
}

// Close is prompt when idle, and harmless twice.
func TestSessionClose(t *testing.T) {
	fakeSessionCLI(t)
	s := startFake(t, &bytes.Buffer{})
	if _, err := Send(s, "go"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := Close(s); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("Close took %s", time.Since(start))
	}
	if err := Close(s); err != nil {
		t.Fatal(err)
	}
}

func TestStartRejectsPrompt(t *testing.T) {
	if _, err := Start(t.TempDir(), RunOpts{Prompt: "hi", ModelID: Default}); err == nil {
		t.Fatal("want error")
	}
}
