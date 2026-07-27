package cliexec

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DebugStamp returns the shared filename prefix for one call's debug dumps,
// millisecond-precise so lexicographic order is chronological order. Taken
// once at the start of a call and reused for all of its files, it groups one
// call's dumps together in a plain ls and keeps repeated calls with the same
// name from overwriting each other.
func DebugStamp() string {
	return time.Now().Format("20060102-150405.000")
}

// DebugName normalises a call name for use in dump filenames: lowercased,
// with anything outside [a-z0-9_-] (spaces, slashes, ...) mapped to '-' so the
// name can't produce a hostile or unwieldy path. Empty names become "unnamed",
// matching the log convention.
func DebugName(name string) string {
	if name == "" {
		return "unnamed"
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '-'
		}
	}, name)
}

// WriteDebugFile writes data to {dir}/{filename}, creating dir if needed,
// best-effort (debug only). No-op when dir is empty.
func WriteDebugFile(dir, filename string, data []byte) {
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
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
