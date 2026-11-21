//go:build unix

package aclio

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeClaude puts a `claude` first on PATH that answers each user message
// with a result counting its turns, exits after its second turn (simulating a
// crash), and appends its --resume arg to starts.txt each time it starts.
func fakeClaude(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
resume=none
while [ $# -gt 0 ]; do [ "$1" = --resume ] && resume=$2; shift; done
echo "$resume" >> '` + dir + `/starts.txt'
n=0
while IFS= read -r line; do
  case "$line" in *'"type":"user"'*)
    n=$((n+1))
    [ -f '` + dir + `/crashed' ] || { [ $n -eq 2 ] && touch '` + dir + `/crashed' && exit 1; }
    printf '{"type":"result","subtype":"success","session_id":"conv","total_cost_usd":0.0%d,"usage":{"input_tokens":1,"output_tokens":1},"structured_output":{"ok":true}}\n' "$n" ;;
  esac
done
`
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

type okResp struct {
	OK bool `json:"ok"`
}

// A Claude session reuses one process, and when it dies mid-turn the turn is
// resent once on a new process resuming the conversation.
func TestClaudeSessionResumesAfterCrash(t *testing.T) {
	dir := fakeClaude(t)
	s, err := StartSession(Request{Provider: Claude, Dir: t.TempDir(), LogWriter: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer CloseSession(s)

	for i := 0; i < 3; i++ {
		v, res, err := SendStructured[okResp](s, "go")
		if err != nil || !v.OK {
			t.Fatalf("turn %d: v = %+v, err = %v", i+1, v, err)
		}
		if res.CostUSD <= 0 {
			t.Errorf("turn %d: CostUSD = %v, want the turn's own (positive) cost", i+1, res.CostUSD)
		}
	}
	starts, _ := os.ReadFile(filepath.Join(dir, "starts.txt"))
	if got := strings.Fields(string(starts)); len(got) != 2 || got[0] != "none" || got[1] != "conv" {
		t.Errorf("starts = %q, want one fresh start then one resume of conv", got)
	}
	if SessionID(s) != "conv" {
		t.Errorf("SessionID = %q", SessionID(s))
	}
	if err := CloseSession(s); err != nil {
		t.Fatal(err)
	}
	if _, err := Send(s, "go"); err == nil {
		t.Fatal("Send after CloseSession: want error")
	}
}

func TestStartSessionChecks(t *testing.T) {
	if _, err := StartSession(Request{Provider: Claude, Prompt: "x"}); err == nil {
		t.Error("Prompt: want error")
	}
	if _, err := StartSession(Request{Provider: "nope"}); err == nil {
		t.Error("provider: want error")
	}
}
