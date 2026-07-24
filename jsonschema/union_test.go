package jsonschema

import (
	"encoding/json"
	"reflect"
	"testing"
)

type mouseLike struct {
	Action   string   `json:"action" enum:"click,move"`
	Selector *string  `json:"selector,omitempty"`
	X        *float64 `json:"x,omitempty"`
}

type keyboardLike struct {
	Action string `json:"action" enum:"type,press"`
	Text   string `json:"text"`
}

type namedA struct {
	Name string `json:"name" description:"a: the name"`
}

type namedB struct {
	Name string `json:"name" description:"b: the name"`
}

func TestUnionOf(t *testing.T) {
	schema, err := UnionOf(mouseLike{}, keyboardLike{})
	if err != nil {
		t.Fatal(err)
	}

	if schema["type"] != "object" || schema["additionalProperties"] != false {
		t.Fatalf("envelope = %v", schema)
	}
	props := schema["properties"].(map[string]any)

	for _, field := range []string{"action", "selector", "x", "text"} {
		if _, ok := props[field]; !ok {
			t.Errorf("missing field %q", field)
		}
	}

	// All fields optional: no required key at all.
	if _, ok := schema["required"]; ok {
		t.Error("union schema should have no required fields")
	}

	gotEnum := props["action"].(map[string]any)["enum"].([]any)
	wantEnum := []any{"click", "move", "type", "press"}
	if !reflect.DeepEqual(gotEnum, wantEnum) {
		t.Errorf("action enum = %v, want %v", gotEnum, wantEnum)
	}
}

func TestUnionOfMergesDescriptions(t *testing.T) {
	schema, err := UnionOf(namedA{}, namedB{})
	if err != nil {
		t.Fatal(err)
	}
	desc := schema["properties"].(map[string]any)["name"].(map[string]any)["description"]
	if desc != "a: the name; b: the name" {
		t.Errorf("description = %q", desc)
	}
}

func TestUnionOfTypeConflict(t *testing.T) {
	type a struct {
		V string `json:"v"`
	}
	type b struct {
		V int `json:"v"`
	}
	if _, err := UnionOf(a{}, b{}); err == nil {
		t.Fatal("expected a type-conflict error")
	}
}

func TestUnionOfDeterministicJSON(t *testing.T) {
	// Generated schemas often end up embedded in prompts; unstable bytes
	// would invalidate prompt caches on every run.
	first, err := UnionOf(mouseLike{}, keyboardLike{})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(first)
	for i := 0; i < 10; i++ {
		next, err := UnionOf(mouseLike{}, keyboardLike{})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(next)
		if string(a) != string(b) {
			t.Fatalf("UnionOf JSON is not deterministic:\n%s\n%s", a, b)
		}
	}
}

func TestEnumTag(t *testing.T) {
	schema, err := FromType(keyboardLike{})
	if err != nil {
		t.Fatal(err)
	}
	enum := schema["properties"].(map[string]any)["action"].(map[string]any)["enum"].([]any)
	if !reflect.DeepEqual(enum, []any{"type", "press"}) {
		t.Errorf("enum = %v", enum)
	}
}
