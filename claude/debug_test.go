package claude

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestDebugDumpFilenames — one call's dumps share the stamp prefix
// ({stamp}-{name}-{kind}), so repeated calls never overwrite each other.
func TestDebugDumpFilenames(t *testing.T) {
	dir := t.TempDir()
	// The raw name needs normalising (case, space) — files must use "my-op".
	const stamp, name, normalised = "20260727-120000.000", "My Op", "my-op"

	debugSettings(dir, stamp, RunOpts{Name: name, Prompt: "secret"})
	debugPrompt(dir, stamp, name, "the prompt")
	debugOut(dir, stamp, name, `{"ok":true}`)
	debugError(dir, stamp, name, errors.New("boom"))

	for _, kind := range []string{"settings.json", "prompt.md", "output.json", "error.txt"} {
		path := filepath.Join(dir, stamp+"-"+normalised+"-"+kind)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing dump %s: %v", path, err)
		}
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

// TestDebugErrorNilNoop — a successful call leaves no error file behind.
func TestDebugErrorNilNoop(t *testing.T) {
	dir := t.TempDir()
	debugError(dir, "s", "op", nil)
	if _, err := os.Stat(filepath.Join(dir, "s-op-error.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("error file written for nil error (stat: %v)", err)
	}
}
