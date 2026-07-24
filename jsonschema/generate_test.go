package jsonschema

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

type embedded struct {
	Inherited string `json:"inherited"`
}

type nested struct {
	Value int `json:"value"`
}

type sample struct {
	embedded
	Name     string          `json:"name" description:"The name"`
	Age      int             `json:"age,omitempty"`
	Score    float64         `json:"score"`
	Active   bool            `json:"active"`
	Tags     []string        `json:"tags"`
	Meta     map[string]int  `json:"meta"`
	Child    nested          `json:"child"`
	MaybePtr *nested         `json:"maybe_ptr,omitempty"`
	When     time.Time       `json:"when"`
	Raw      json.RawMessage `json:"raw"`
	Anything any             `json:"anything"`
	Stringed int             `json:"stringed,string"`
	Skipped  string          `json:"-"`
	ignored  string          //lint:ignore U1000 exercised via reflection
}

func TestFromType(t *testing.T) {
	schema, err := FromType(sample{})
	if err != nil {
		t.Fatal(err)
	}

	if schema["type"] != "object" {
		t.Fatalf("top-level type = %v, want object", schema["type"])
	}
	if schema["additionalProperties"] != false {
		t.Fatalf("additionalProperties = %v, want false", schema["additionalProperties"])
	}

	props := schema["properties"].(map[string]any)

	checks := map[string]string{
		"inherited": "string",
		"name":      "string",
		"age":       "integer",
		"score":     "number",
		"active":    "boolean",
		"tags":      "array",
		"meta":      "object",
		"child":     "object",
		"maybe_ptr": "object",
		"when":      "string",
		"stringed":  "string",
	}
	for field, wantType := range checks {
		prop, ok := props[field].(map[string]any)
		if !ok {
			t.Errorf("missing property %q", field)
			continue
		}
		if prop["type"] != wantType {
			t.Errorf("property %q type = %v, want %v", field, prop["type"], wantType)
		}
	}

	if _, present := props["Skipped"]; present {
		t.Error("json:\"-\" field was not skipped")
	}
	if _, present := props["ignored"]; present {
		t.Error("unexported field was not skipped")
	}

	if desc := props["name"].(map[string]any)["description"]; desc != "The name" {
		t.Errorf("name description = %v", desc)
	}
	if format := props["when"].(map[string]any)["format"]; format != "date-time" {
		t.Errorf("time.Time format = %v, want date-time", format)
	}
	if raw := props["raw"].(map[string]any); len(raw) != 0 {
		t.Errorf("json.RawMessage schema = %v, want empty (any)", raw)
	}
	if anything := props["anything"].(map[string]any); len(anything) != 0 {
		t.Errorf("any schema = %v, want empty (any)", anything)
	}

	required := schema["required"].([]string)
	wantRequired := []string{"inherited", "name", "score", "active", "tags", "meta", "child", "when", "raw", "anything", "stringed"}
	if !reflect.DeepEqual(required, wantRequired) {
		t.Errorf("required = %v, want %v", required, wantRequired)
	}
}

func TestFromTypeScalars(t *testing.T) {
	schema, err := FromType("")
	if err != nil {
		t.Fatal(err)
	}
	if schema["type"] != "string" {
		t.Errorf("string schema type = %v", schema["type"])
	}
}

type recursive struct {
	Next *recursive `json:"next,omitempty"`
}

func TestFromTypeRecursive(t *testing.T) {
	if _, err := FromType(recursive{}); err == nil {
		t.Fatal("expected an error for a recursive type")
	}
}

func TestFromTypeNil(t *testing.T) {
	if _, err := FromType(nil); err == nil {
		t.Fatal("expected an error for untyped nil")
	}
}

func TestFromTypeJSONRoundTrip(t *testing.T) {
	data, err := FromTypeJSON(nested{})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("generated schema is not valid JSON: %v", err)
	}
}
