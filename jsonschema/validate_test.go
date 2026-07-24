package jsonschema

import "testing"

type person struct {
	Name string `json:"name"`
	Age  int    `json:"age,omitempty"`
}

func TestValidate(t *testing.T) {
	schema, err := FromTypeJSON(person{})
	if err != nil {
		t.Fatal(err)
	}

	if err := Validate(schema, []byte(`{"name": "Ada", "age": 36}`)); err != nil {
		t.Errorf("valid document rejected: %v", err)
	}
	if err := Validate(schema, []byte(`{"age": 36}`)); err == nil {
		t.Error("document missing required field accepted")
	}
	if err := Validate(schema, []byte(`{"name": "Ada", "extra": true}`)); err == nil {
		t.Error("document with unknown field accepted despite additionalProperties: false")
	}
	if err := Validate(schema, []byte(`{"name": 42}`)); err == nil {
		t.Error("document with wrong type accepted")
	}
}

func TestValidateValue(t *testing.T) {
	schema, err := FromTypeJSON(person{})
	if err != nil {
		t.Fatal(err)
	}

	if err := ValidateValue(schema, person{Name: "Ada"}); err != nil {
		t.Errorf("valid value rejected: %v", err)
	}
	if err := ValidateValue(schema, map[string]any{"name": 42}); err == nil {
		t.Error("invalid value accepted")
	}
}

func TestValidateBadSchema(t *testing.T) {
	if err := Validate([]byte(`{`), []byte(`{}`)); err == nil {
		t.Error("broken schema accepted")
	}
}
