package jsonschema

import (
	"fmt"
	"reflect"
)

// UnionOf builds a single object schema holding the union of the given
// structs' fields, all optional. This is the loose-payload half of a
// discriminated union — a {type, data} envelope where which of data's fields
// are valid is decided by type and enforced by the decoder, while the schema
// stays one flat object (the dialect Claude's structured outputs accept has
// no oneOf-per-variant support worth leaning on).
//
// Fields sharing a name across structs must agree on their schema "type";
// their enum values are unioned (first-seen order) and their descriptions
// concatenated.
func UnionOf(values ...any) (map[string]any, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("jsonschema: UnionOf needs at least one value")
	}

	merged := map[string]any{}
	for _, v := range values {
		t := reflect.TypeOf(v)
		if t == nil {
			return nil, fmt.Errorf("jsonschema: UnionOf: untyped nil value")
		}
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			return nil, fmt.Errorf("jsonschema: UnionOf: %s is not a struct", t)
		}

		schema, err := FromReflectType(t)
		if err != nil {
			return nil, err
		}
		props, _ := schema["properties"].(map[string]any)
		for name, raw := range props {
			prop := raw.(map[string]any)
			existing, ok := merged[name].(map[string]any)
			if !ok {
				merged[name] = prop
				continue
			}
			if err := mergeProperty(existing, prop); err != nil {
				return nil, fmt.Errorf("jsonschema: UnionOf: field %q: %w", name, err)
			}
		}
	}

	return map[string]any{
		"type":                 "object",
		"properties":           merged,
		"additionalProperties": false,
	}, nil
}

// mergeProperty folds prop into existing: types must match, enums union,
// descriptions concatenate.
func mergeProperty(existing, prop map[string]any) error {
	if existing["type"] != prop["type"] {
		return fmt.Errorf("conflicting types %v and %v", existing["type"], prop["type"])
	}

	if enum, ok := prop["enum"].([]any); ok {
		existingEnum, _ := existing["enum"].([]any)
		seen := map[any]bool{}
		for _, v := range existingEnum {
			seen[v] = true
		}
		for _, v := range enum {
			if !seen[v] {
				existingEnum = append(existingEnum, v)
				seen[v] = true
			}
		}
		existing["enum"] = existingEnum
	}

	if desc, ok := prop["description"].(string); ok && desc != "" {
		existingDesc, _ := existing["description"].(string)
		switch {
		case existingDesc == "":
			existing["description"] = desc
		case existingDesc != desc:
			existing["description"] = existingDesc + "; " + desc
		}
	}

	return nil
}
