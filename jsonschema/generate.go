// Package jsonschema generates JSON Schemas from Go types (driven by `json`
// struct tags) and validates JSON documents against schemas.
//
// Generated schemas target the dialect Claude's structured outputs accept:
// every object carries additionalProperties: false, required lists the
// non-omitempty fields, and recursive types are rejected (the API doesn't
// support recursive schemas).
package jsonschema

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// FromType builds a JSON Schema for the dynamic type of v, following the same
// `json` struct-tag rules encoding/json uses: tag names, `-` skips, omitempty
// makes a field optional, `,string` encodes as a string, anonymous embedded
// structs are flattened. Two extra struct tags are honoured: a
// `description:"..."` tag becomes the property's description, and an
// `enum:"a,b,c"` tag constrains the property to those values.
func FromType(v any) (map[string]any, error) {
	t := reflect.TypeOf(v)
	if t == nil {
		return nil, fmt.Errorf("jsonschema: cannot derive a schema from an untyped nil")
	}
	return FromReflectType(t)
}

// FromTypeJSON is FromType marshalled to JSON, ready to hand to a validator
// or the claude CLI's --json-schema flag.
func FromTypeJSON(v any) ([]byte, error) {
	schema, err := FromType(v)
	if err != nil {
		return nil, err
	}
	return json.Marshal(schema)
}

// FromReflectType builds a JSON Schema for t; see FromType.
func FromReflectType(t reflect.Type) (map[string]any, error) {
	return schemaFor(t, map[reflect.Type]bool{})
}

var (
	timeType       = reflect.TypeOf(time.Time{})
	rawMessageType = reflect.TypeOf(json.RawMessage{})
	marshalerType  = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
)

func schemaFor(t reflect.Type, visiting map[reflect.Type]bool) (map[string]any, error) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	switch t {
	case timeType:
		return map[string]any{"type": "string", "format": "date-time"}, nil
	case rawMessageType:
		return map[string]any{}, nil
	}
	// A custom marshaller can emit any shape; the honest schema is "anything".
	if t != timeType && (t.Implements(marshalerType) || reflect.PointerTo(t).Implements(marshalerType)) {
		return map[string]any{}, nil
	}

	switch t.Kind() {
	case reflect.Bool:
		return map[string]any{"type": "boolean"}, nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}, nil

	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}, nil

	case reflect.String:
		return map[string]any{"type": "string"}, nil

	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			// encoding/json emits []byte as a base64 string.
			return map[string]any{"type": "string"}, nil
		}
		items, err := schemaFor(t.Elem(), visiting)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "array", "items": items}, nil

	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("jsonschema: unsupported map key type %s (only string keys)", t.Key())
		}
		values, err := schemaFor(t.Elem(), visiting)
		if err != nil {
			return nil, err
		}
		// Note: Claude structured outputs only accept additionalProperties:
		// false, so open maps can't be expressed there — prefer a struct.
		return map[string]any{"type": "object", "additionalProperties": values}, nil

	case reflect.Interface:
		return map[string]any{}, nil

	case reflect.Struct:
		if visiting[t] {
			return nil, fmt.Errorf("jsonschema: recursive type %s is not supported", t)
		}
		visiting[t] = true
		defer delete(visiting, t)

		properties := map[string]any{}
		required := []string{}
		if err := addStructFields(t, properties, &required, visiting); err != nil {
			return nil, err
		}
		schema := map[string]any{
			"type":                 "object",
			"properties":           properties,
			"additionalProperties": false,
		}
		if len(required) > 0 {
			schema["required"] = required
		}
		return schema, nil

	default:
		return nil, fmt.Errorf("jsonschema: unsupported type %s (kind %s)", t, t.Kind())
	}
}

// addStructFields walks t's fields into properties/required, recursing into
// anonymous embedded structs the way encoding/json flattens them.
func addStructFields(t reflect.Type, properties map[string]any, required *[]string, visiting map[reflect.Type]bool) error {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("json")

		if tag == "-" {
			continue
		}

		name, opts := parseJSONTag(tag)

		// An embedded struct without its own tag name is flattened — like
		// encoding/json, this also promotes the exported fields of an
		// unexported embedded struct type.
		if field.Anonymous && name == "" {
			embedded := field.Type
			for embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct {
				if err := addStructFields(embedded, properties, required, visiting); err != nil {
					return err
				}
				continue
			}
		}

		if !field.IsExported() {
			continue
		}

		if name == "" {
			name = field.Name
		}

		var prop map[string]any
		if opts["string"] {
			prop = map[string]any{"type": "string"}
		} else {
			var err error
			prop, err = schemaFor(field.Type, visiting)
			if err != nil {
				return fmt.Errorf("field %s.%s: %w", t.Name(), field.Name, err)
			}
		}

		if desc := field.Tag.Get("description"); desc != "" {
			prop["description"] = desc
		}
		if enum := field.Tag.Get("enum"); enum != "" {
			values := strings.Split(enum, ",")
			anyValues := make([]any, len(values))
			for i, v := range values {
				anyValues[i] = strings.TrimSpace(v)
			}
			prop["enum"] = anyValues
		}

		properties[name] = prop
		if !opts["omitempty"] {
			*required = append(*required, name)
		}
	}
	return nil
}

// parseJSONTag splits a `json` tag into its name and option set.
func parseJSONTag(tag string) (name string, opts map[string]bool) {
	opts = map[string]bool{}
	parts := strings.Split(tag, ",")
	name = parts[0]
	for _, opt := range parts[1:] {
		opts[opt] = true
	}
	return name, opts
}
