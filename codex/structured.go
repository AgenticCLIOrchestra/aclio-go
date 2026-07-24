package codex

import (
	"encoding/json"
	"fmt"

	"github.com/agenticcliorchestra/aclio-go/jsonschema"
)

// ParseStructured decodes a Codex final-message string (the JSON document
// produced under an output schema) into T.
func ParseStructured[T any](finalText string) (T, error) {
	var v T
	if err := json.Unmarshal([]byte(finalText), &v); err != nil {
		return v, fmt.Errorf("decoding codex structured output: %w", err)
	}
	return v, nil
}

// RunStructured runs `codex exec` requiring structured output of type T. When
// opts.OutputSchema is empty, the schema is generated from T's json tags via
// jsonschema.FromType; set it explicitly when the schema can't be derived from
// the type (e.g. discriminated unions behind interface fields). Returns the
// decoded output alongside the RunResult (thread id, usage).
func RunStructured[T any](absDir string, opts RunOpts) (T, *RunResult, error) {
	var zero T

	if opts.OutputSchema == "" {
		schema, err := jsonschema.FromTypeJSON(zero)
		if err != nil {
			return zero, nil, fmt.Errorf("generating schema for %T: %w", zero, err)
		}
		opts.OutputSchema = string(schema)
	}

	finalText, result, err := Run(absDir, opts)
	if err != nil {
		return zero, nil, err
	}

	v, err := ParseStructured[T](finalText)
	if err != nil {
		return zero, result, err
	}
	return v, result, nil
}
