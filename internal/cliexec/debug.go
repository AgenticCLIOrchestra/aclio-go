package cliexec

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

// WriteDebugFile writes data to {dir}/{filename}, best-effort (debug only).
// No-op when dir is empty.
func WriteDebugFile(dir, filename string, data []byte) {
	if dir == "" {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, filename), data, 0644)
}

// DebugValue writes v as pretty JSON to {dir}/{filename}. No-op when dir is
// empty or v doesn't marshal.
func DebugValue(dir, filename string, v any) {
	if dir == "" {
		return
	}
	data, err := json.MarshalIndent(v, "", "    ")
	if err != nil {
		return
	}
	WriteDebugFile(dir, filename, data)
}

// DebugString writes s verbatim to {dir}/{filename}. No-op when dir is empty.
func DebugString(dir, filename, s string) {
	WriteDebugFile(dir, filename, []byte(s))
}

// DebugMaybeJSON writes s to {dir}/{filename}, pretty-printed when it is valid
// JSON and raw otherwise. No-op when dir is empty.
func DebugMaybeJSON(dir, filename, s string) {
	if dir == "" {
		return
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, []byte(s), "", "    "); err == nil {
		WriteDebugFile(dir, filename, pretty.Bytes())
		return
	}
	WriteDebugFile(dir, filename, []byte(s))
}
