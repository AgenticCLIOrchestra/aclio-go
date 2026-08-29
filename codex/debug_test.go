package codex

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/agenticcliorchestra/aclio-go/internal/cliexec"
)

// TestWriteSchemaFileStampPrefixed — the schema file shares the call's stamp
// prefix, so repeated calls into the same TempDir don't overwrite each other,
// and the dir is created if missing.
func TestWriteSchemaFileStampPrefixed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dbg")
	opts := RunOpts{Name: "op", OutputSchema: `{"type":"object"}`, TempDir: dir}

	path, cleanup, err := writeSchemaFile(opts, "20260727-120000.000")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	want := filepath.Join(dir, "20260727-120000.000-op-schema.json")
	if path != want {
		t.Errorf("schema path = %q, want %q", path, want)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("schema file not written: %v", err)
	}
}

// TestDebugSettingsBlanksPrompt — the prompt is dumped separately, so the
// settings JSON must not duplicate it.
func TestDebugSettingsBlanksPrompt(t *testing.T) {
	dir := t.TempDir()
	debugSettings(dir, "s", RunOpts{Name: "op", Prompt: "secret"})

	data, err := os.ReadFile(filepath.Join(dir, "s-op-settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var dumped RunOpts
	if err := json.Unmarshal(data, &dumped); err != nil {
		t.Fatal(err)
	}
	if dumped.Prompt != "" {
		t.Errorf("settings dump kept the prompt %q; want it blanked", dumped.Prompt)
	}
}

// captureLog redirects the shared log sink into a buffer for the duration of
// fn and returns what was written.
func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	orig := cliexec.LogWriter
	cliexec.LogWriter = &buf
	t.Cleanup(func() { cliexec.LogWriter = orig })
	fn()
	return buf.String()
}

// TestLogStart — one greppable [codex] [start] line per call, carrying the op
// name (pairing with the [codex] [usage] completion line), model, and prompt
// size. An unset model logs as "default".
func TestLogStart(t *testing.T) {
	got := captureLog(t, func() {
		logStart(RunOpts{Name: "triage", Model: "o3", Prompt: "12345"}, "[codex] [triage]")
	})
	want := "[codex] [triage] [start] op=triage model=o3 prompt_chars=5\n"
	if got != want {
		t.Errorf("logStart wrote %q, want %q", got, want)
	}

	got = captureLog(t, func() {
		logStart(RunOpts{}, "[codex]")
	})
	want = "[codex] [start] op=unnamed model=default prompt_chars=0\n"
	if got != want {
		t.Errorf("logStart wrote %q, want %q", got, want)
	}
}
