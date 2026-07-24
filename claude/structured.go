package claude

import (
	"fmt"

	"github.com/agenticcliorchestra/aclio-go/jsonschema"
)

// ParseStructured decodes a claude CLI result blob into T alongside the full
// result (session id, usage, cost). It replaces the per-type parse helpers
// that call ParseResult and Structured by hand.
func ParseStructured[T any](cliOutput string) (T, *RunResult, error) {
	var v T

	parsed, err := ParseResult(cliOutput)
	if err != nil {
		return v, nil, fmt.Errorf("parsing claude output: %w", err)
	}

	if err := Structured(cliOutput, &v); err != nil {
		return v, parsed, err
	}
	return v, parsed, nil
}

// RunStructured runs the claude CLI requiring structured output of type T.
// When opts.JsonSchema is empty, the schema is generated from T's json tags
// via jsonschema.FromType; set it explicitly when the schema can't be derived
// from the type (e.g. discriminated unions behind interface fields). Returns
// the decoded structured output alongside the full result blob (session ID,
// usage, cost).
func RunStructured[T any](absDir string, opts RunOpts) (T, *RunResult, error) {
	if opts.JsonSchema == "" {
		var zero T
		schema, err := jsonschema.FromTypeJSON(zero)
		if err != nil {
			return zero, nil, fmt.Errorf("generating schema for %T: %w", zero, err)
		}
		opts.JsonSchema = string(schema)
	}

	out, err := Run(absDir, opts)
	if err != nil {
		var zero T
		return zero, nil, err
	}

	return ParseStructured[T](out)
}
