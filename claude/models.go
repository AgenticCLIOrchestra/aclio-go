package claude

import (
	"regexp"
	"strings"
)

// Model identifies the model passed to the claude CLI via --model. Aliases
// (opus, sonnet, ...) track the latest model of a tier; full claude-* IDs pin
// a specific version.
type Model string

const (
	Default  Model = "default"
	Opus     Model = "opus"
	Sonnet   Model = "sonnet"
	Haiku    Model = "haiku"
	OpusPlan Model = "opusplan"

	// Pinned model IDs, current as of 2026-07.
	Fable5  Model = "claude-fable-5"
	Opus48  Model = "claude-opus-4-8"
	Sonnet5 Model = "claude-sonnet-5"
	Haiku45 Model = "claude-haiku-4-5-20251001"
)

var modelAliases = map[Model]bool{
	Default:  true,
	Opus:     true,
	Sonnet:   true,
	Haiku:    true,
	OpusPlan: true,
}

// modelIDRegex matches claude-* model IDs without pinning a version list, so
// the library doesn't go stale when new models ship. The character set is
// tight enough that the value can't smuggle extra CLI flags.
var modelIDRegex = regexp.MustCompile(`^claude-[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)

// IsValidModel reports whether m is an accepted --model value: a known alias
// or a claude-* model ID, optionally carrying the [1m] context suffix.
func IsValidModel(m Model) bool {
	id := strings.TrimSuffix(string(m), "[1m]")
	if modelAliases[Model(id)] {
		return true
	}
	return modelIDRegex.MatchString(id)
}
