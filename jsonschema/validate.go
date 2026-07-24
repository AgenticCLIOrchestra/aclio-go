package jsonschema

import (
	"bytes"
	"encoding/json"
	"fmt"

	jsv "github.com/santhosh-tekuri/jsonschema/v6"
)

// Validate checks that the JSON document doc conforms to the JSON Schema
// schema (both raw JSON bytes). A nil return means the document is valid; a
// non-nil error describes either a broken schema or the first validation
// failure.
func Validate(schema, doc []byte) error {
	compiled, err := compile(schema)
	if err != nil {
		return err
	}
	instance, err := jsv.UnmarshalJSON(bytes.NewReader(doc))
	if err != nil {
		return fmt.Errorf("jsonschema: parsing document: %w", err)
	}
	return compiled.Validate(instance)
}

// ValidateValue checks that v (any Go value — struct, map, slice, ...)
// conforms to schema after being marshalled to JSON. Useful for asserting a
// value you're about to send matches the schema you advertised.
func ValidateValue(schema []byte, v any) error {
	doc, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("jsonschema: marshalling value: %w", err)
	}
	return Validate(schema, doc)
}

func compile(schema []byte) (*jsv.Schema, error) {
	parsed, err := jsv.UnmarshalJSON(bytes.NewReader(schema))
	if err != nil {
		return nil, fmt.Errorf("jsonschema: parsing schema: %w", err)
	}
	compiler := jsv.NewCompiler()
	if err := compiler.AddResource("schema.json", parsed); err != nil {
		return nil, fmt.Errorf("jsonschema: adding schema resource: %w", err)
	}
	compiled, err := compiler.Compile("schema.json")
	if err != nil {
		return nil, fmt.Errorf("jsonschema: compiling schema: %w", err)
	}
	return compiled, nil
}
