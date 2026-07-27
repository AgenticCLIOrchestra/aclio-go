package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
