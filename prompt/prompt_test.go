package prompt

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestTextAndJSONSubstitute(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}
	tpl := "User: {user}\nData:\n{data}"
	res, err := Render(tpl, map[string]Param{
		"user": Text("ada"),
		"data": JSON(payload{Name: "x", N: 2}),
	})
	if err != nil {
		t.Fatal(err)
	}

	wantLLM := "User: ada\nData:\n{\"name\":\"x\",\"n\":2}"
	if res.LLM != wantLLM {
		t.Errorf("LLM =\n%q\nwant\n%q", res.LLM, wantLLM)
	}
	wantDebug := "User: ada\nData:\n{\n  \"name\": \"x\",\n  \"n\": 2\n}"
	if res.Debug != wantDebug {
		t.Errorf("Debug =\n%q\nwant\n%q", res.Debug, wantDebug)
	}
	if len(res.Unreplaced) != 0 {
		t.Errorf("Unreplaced = %v, want none", res.Unreplaced)
	}
}

func TestMultipleOccurrences(t *testing.T) {
	res, err := Render("{x} and {x} and {x}", map[string]Param{"x": Text("Q")})
	if err != nil {
		t.Fatal(err)
	}
	if res.LLM != "Q and Q and Q" {
		t.Errorf("LLM = %q", res.LLM)
	}
}

func TestDeadParamErrors(t *testing.T) {
	_, err := Render("hello {a}", map[string]Param{
		"a":        Text("1"),
		"b_typoed": Text("2"),
	})
	if err == nil {
		t.Fatal("expected an error for a dead param")
	}
	if !strings.Contains(err.Error(), "b_typoed") {
		t.Errorf("error should name the dead param: %v", err)
	}
}

func TestZeroValueParamErrors(t *testing.T) {
	_, err := Render("hi {x}", map[string]Param{"x": {}})
	if err == nil {
		t.Fatal("expected an error for a zero-value Param")
	}
	if !strings.Contains(err.Error(), "x") {
		t.Errorf("error should name the param: %v", err)
	}
}

func TestJSONMarshalFailure(t *testing.T) {
	type bad struct {
		Ch chan int `json:"ch"`
	}
	_, err := Render("{x}", map[string]Param{"x": JSON(bad{})})
	if err == nil {
		t.Fatal("expected a marshal error for a channel field")
	}
}

func TestPreEncodedJSON(t *testing.T) {
	// []byte (and json.RawMessage) are the pre-encoded carriers: normalised
	// from raw bytes, so whitespace is fixed but key order is preserved.
	res, err := Render("{x}", map[string]Param{
		"x": JSON([]byte(`{"b":2,   "a":1}`)), // sloppy spacing, deliberate key order
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.LLM != `{"b":2,"a":1}` {
		t.Errorf("LLM = %q, want compacted with key order preserved", res.LLM)
	}
	if res.Debug != "{\n  \"b\": 2,\n  \"a\": 1\n}" {
		t.Errorf("Debug = %q", res.Debug)
	}

	if _, err := Render("{x}", map[string]Param{"x": JSON([]byte(`{not json`))}); err == nil {
		t.Error("expected an error for malformed pre-encoded JSON")
	}

	// json.Compact/Indent don't HTML-escape, so < > & survive the []byte path
	// just as they do the Go-value path (see TestHTMLCharsSurvive).
	htmlRes, err := Render("{x}", map[string]Param{"x": JSON([]byte(`{"q": "a < b & c > d"}`))})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(htmlRes.LLM, "a < b & c > d") {
		t.Errorf("HTML chars escaped in pre-encoded LLM: %q", htmlRes.LLM)
	}
	if !strings.Contains(htmlRes.Debug, "a < b & c > d") {
		t.Errorf("HTML chars escaped in pre-encoded Debug: %q", htmlRes.Debug)
	}
}

func TestStringMarshaledAsJSONString(t *testing.T) {
	// A string is a normal Go value: json.Marshal-ed, never treated as
	// pre-encoded. This is the predictable contract — no coercion surprises.
	cases := map[string]string{
		"hello": `"hello"`, // text → quoted JSON string, not an error
		"123":   `"123"`,   // digits → quoted string, NOT the number 123
		"true":  `"true"`,  // NOT the boolean true
		`a "b"`: `"a \"b\""`,
	}
	for in, want := range cases {
		res, err := Render("{x}", map[string]Param{"x": JSON(in)})
		if err != nil {
			t.Fatalf("JSON(%q): unexpected error %v", in, err)
		}
		if res.LLM != want {
			t.Errorf("JSON(%q).LLM = %s, want %s", in, res.LLM, want)
		}
		// Scalar has no whitespace to indent, so both renderings match.
		if res.Debug != want {
			t.Errorf("JSON(%q).Debug = %s, want %s", in, res.Debug, want)
		}
	}
}

func TestHTMLCharsSurvive(t *testing.T) {
	// Go-value path: the encoder runs with SetEscapeHTML(false), so < > &
	// must survive intact in both renderings (prompts are Markdown-bound).
	res, err := Render("{x}", map[string]Param{
		"x": JSON(map[string]string{"q": "a < b && c > d"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.LLM, "a < b && c > d") {
		t.Errorf("HTML chars were escaped in LLM: %q", res.LLM)
	}
	if !strings.Contains(res.Debug, "a < b && c > d") {
		t.Errorf("HTML chars were escaped in Debug: %q", res.Debug)
	}
}

func TestJSONExampleBlocksUntouched(t *testing.T) {
	// A JSON example in the template must not be treated as a placeholder or
	// reported as a leftover — only {lower_snake_case} tokens are.
	tpl := "Return shape: {\"key\": \"value\"} and ${shellVar} and { }.\nFill: {answer}"
	res, err := Render(tpl, map[string]Param{"answer": Text("42")})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.LLM, `{"key": "value"}`) {
		t.Errorf("JSON example was altered: %q", res.LLM)
	}
	if len(res.Unreplaced) != 0 {
		t.Errorf("Unreplaced = %v, want none (examples must not be flagged)", res.Unreplaced)
	}
}

func TestLeftoverDetection(t *testing.T) {
	tpl := "bind {a}, leave {unbound_one} and {unbound_two} and {unbound_one} again"
	res, err := Render(tpl, map[string]Param{"a": Text("x")})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"{unbound_one}", "{unbound_two}"}
	if !slices.Equal(res.Unreplaced, want) {
		t.Errorf("Unreplaced = %v, want %v (deduped, first-occurrence order)", res.Unreplaced, want)
	}

	if _, err := RenderStrict(tpl, map[string]Param{"a": Text("x")}); err == nil {
		t.Error("RenderStrict should error on leftover placeholders")
	}
}

func TestValueNotReSubstituted(t *testing.T) {
	// A param value that contains a {placeholder}-shaped token must not trigger
	// a second substitution (injection-proof), and must not be reported as a
	// leftover: leftovers are computed from the template, not the output, so
	// {ghost} — which appears only inside a value, never in the template — is
	// data, not an unbound placeholder.
	res, err := Render("{a}", map[string]Param{
		"a": Text("see the {ghost} placeholder"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.LLM != "see the {ghost} placeholder" {
		t.Errorf("LLM = %q; value {ghost} must stay literal, not re-substituted", res.LLM)
	}
	if len(res.Unreplaced) != 0 {
		t.Errorf("Unreplaced = %v, want none: {ghost} came from a value, not the template", res.Unreplaced)
	}
}

func TestTextOnlyRenderingsIdentical(t *testing.T) {
	res, err := Render("{a} / {b} / {a}", map[string]Param{
		"a": Text("one"),
		"b": Text("two"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.LLM != res.Debug {
		t.Errorf("with only Text params LLM and Debug must be identical:\nLLM=%q\nDebug=%q", res.LLM, res.Debug)
	}
}

func TestRawMessageParam(t *testing.T) {
	res, err := Render("{x}", map[string]Param{
		"x": JSON(json.RawMessage(`{"a": 1}`)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.LLM != `{"a":1}` {
		t.Errorf("LLM = %q", res.LLM)
	}
}
