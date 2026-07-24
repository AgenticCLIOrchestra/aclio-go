// Package prompt substitutes parameters into a prompt template and produces
// two renderings of the result in one pass: an LLM rendering (JSON params
// compact, to save tokens at the model boundary) and a Debug rendering (JSON
// params two-space indented, for human-readable dumps). The two are
// byte-identical except inside JSON param values, so a debug dump faithfully
// shows what the model saw.
//
// It is runner-agnostic — no dependency on the claude or codex packages, and
// stdlib only. Callers pass Result.LLM as the runner's prompt and Result.Debug
// to their dump mechanism.
//
// This is not a template language: it does placeholder substitution and JSON
// encoding policy, nothing else (no conditionals, loops, or text/template).
package prompt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// placeholderRegex matches a {lower_snake_case} placeholder. The tight pattern
// is what makes leftover detection tractable: JSON/code fragments in templates
// (`{"key": "value"}`, `${var}`, `{ }`) never match, so they are neither
// substituted nor flagged.
var placeholderRegex = regexp.MustCompile(`\{[a-z][a-z0-9_]*\}`)

// keyRegex validates a bare param key (the inside of a placeholder).
var keyRegex = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type kind int

const (
	kindInvalid kind = iota // the zero value — a Param constructed via the struct literal
	kindText
	kindJSON
)

// Param is one template parameter. Construct via Text or JSON — the zero value
// is invalid and Render rejects it.
type Param struct {
	kind  kind
	value any
}

// Text substitutes s verbatim.
func Text(s string) Param {
	return Param{kind: kindText, value: s}
}

// JSON substitutes the JSON encoding of v: compact in the LLM rendering,
// two-space indented in the debug rendering.
//
// v is encoded with json.Marshal, so it behaves predictably for every Go
// value — a string becomes a JSON string (JSON("hello") renders "hello",
// JSON("123") renders "123"), a struct or map becomes a JSON object, and so
// on. The only exceptions are []byte and json.RawMessage, which are treated as
// JSON that is *already* encoded: they are validated and re-emitted (compact /
// indented) rather than base64-encoded, preserving key order and number
// formatting. Malformed pre-encoded JSON is a Render error, not silent garbage
// in the prompt.
//
// To substitute text verbatim (not as JSON) use Text. To pass JSON you already
// hold, wrap it as json.RawMessage.
func JSON(v any) Param {
	return Param{kind: kindJSON, value: v}
}

// Result holds both renderings a single Render call produces from one set of
// params — LLM for the model, Debug for dumps — plus any unbound placeholders.
type Result struct {
	// LLM is the prompt to send to the model: JSON params compact.
	LLM string
	// Debug is the same prompt for dumps/logs: JSON params two-space indented.
	// Byte-identical to LLM except inside JSON param values.
	Debug string
	// Unreplaced lists template placeholders ({lower_snake_case}) that no param
	// bound, deduplicated in first-occurrence order. It is computed from the
	// template, not the substituted output, so placeholder-shaped text arriving
	// inside a param value is data and is never reported here. Empty on a clean
	// render. Render populates it; RenderStrict turns it into an error.
	Unreplaced []string
}

// Render substitutes params into tpl.
//
// It errors when:
//   - a param key does not occur in tpl as {key} (dead param — a typo or a
//     stale template),
//   - a JSON param fails to marshal, or pre-encoded JSON fails to parse,
//   - a Param is the zero value.
//
// Leftover placeholder-shaped tokens do NOT error here; they are reported in
// Result.Unreplaced for the caller to judge.
func Render(tpl string, params map[string]Param) (Result, error) {
	// Encode every param up front (both renderings) so a bad param fails
	// before any substitution, and validate each key is actually present.
	llmVals := make(map[string]string, len(params))
	debugVals := make(map[string]string, len(params))

	for key, p := range params {
		if !keyRegex.MatchString(key) {
			return Result{}, fmt.Errorf("prompt: invalid param key %q (want lower snake_case)", key)
		}
		placeholder := "{" + key + "}"
		if !strings.Contains(tpl, placeholder) {
			return Result{}, fmt.Errorf("prompt: dead param %q: %s does not occur in the template", key, placeholder)
		}

		llm, debug, err := encodeParam(key, p)
		if err != nil {
			return Result{}, err
		}
		llmVals[key] = llm
		debugVals[key] = debug
	}

	llm := substitute(tpl, llmVals)
	debug := substitute(tpl, debugVals)

	return Result{
		LLM:        llm,
		Debug:      debug,
		Unreplaced: leftovers(tpl, params),
	}, nil
}

// RenderStrict is Render, but a non-empty Result.Unreplaced is an error. Use it
// when the template is fully owned by the caller and every placeholder must be
// bound.
func RenderStrict(tpl string, params map[string]Param) (Result, error) {
	res, err := Render(tpl, params)
	if err != nil {
		return res, err
	}
	if len(res.Unreplaced) > 0 {
		return res, fmt.Errorf("prompt: unbound placeholders: %s", strings.Join(res.Unreplaced, ", "))
	}
	return res, nil
}

// encodeParam produces the LLM and Debug substitution values for one param.
func encodeParam(key string, p Param) (llm, debug string, err error) {
	switch p.kind {
	case kindText:
		s := p.value.(string)
		return s, s, nil
	case kindJSON:
		// Encode once (compact) and derive the indented Debug form from those
		// bytes: one marshal per param, and the two renderings become
		// structurally incapable of diverging.
		compact, err := encodeJSON(p.value)
		if err != nil {
			return "", "", fmt.Errorf("prompt: param %q: %w", key, err)
		}
		var buf bytes.Buffer
		if err := json.Indent(&buf, []byte(compact), "", "  "); err != nil {
			// compact is valid JSON by construction, so this is unreachable in
			// practice; surface it rather than silently drop it.
			return "", "", fmt.Errorf("prompt: param %q: indenting JSON: %w", key, err)
		}
		return compact, buf.String(), nil
	default:
		return "", "", fmt.Errorf("prompt: param %q is the zero value (construct with Text or JSON)", key)
	}
}

// encodeJSON renders v as compact JSON (encodeParam derives the indented Debug
// form from this output). []byte and json.RawMessage already holding JSON are
// compacted from their raw bytes (preserving key order and number formatting);
// everything else — including string — goes through json.Marshal. HTML escaping
// is disabled — prompts are Markdown-bound.
func encodeJSON(v any) (string, error) {
	if raw, ok := rawJSONBytes(v); ok {
		var buf bytes.Buffer
		if err := json.Compact(&buf, raw); err != nil {
			return "", fmt.Errorf("invalid pre-encoded JSON: %w", err)
		}
		return buf.String(), nil
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	// Encoder appends a trailing newline; the prompt substitution wants none.
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

// rawJSONBytes returns the raw JSON bytes of v when v is a pre-encoded JSON
// carrier (json.RawMessage or []byte). A string is deliberately NOT pre-encoded
// — it goes through json.Marshal like any other Go value (so JSON("hi") renders
// "hi", not hi), which keeps the API predictable. Pass json.RawMessage to
// substitute JSON you already hold.
func rawJSONBytes(v any) ([]byte, bool) {
	switch t := v.(type) {
	case json.RawMessage:
		return []byte(t), true
	case []byte:
		return t, true
	default:
		return nil, false
	}
}

// substitute replaces every {key} in tpl with its value. Values are never
// re-scanned, so a value containing {other} does not trigger further
// substitution — rendering is order-independent and payload content cannot
// smuggle in a placeholder.
func substitute(tpl string, vals map[string]string) string {
	return placeholderRegex.ReplaceAllStringFunc(tpl, func(match string) string {
		key := match[1 : len(match)-1] // strip { }
		if v, ok := vals[key]; ok {
			return v
		}
		return match // leftover — left as-is, reported via leftovers()
	})
}

// leftovers returns the template placeholders in tpl that no param in bound
// resolved, deduplicated in first-occurrence order. It scans the template, not
// the substituted output, so placeholder-shaped text arriving inside a param
// value is treated as data and never reported — data content cannot poison the
// result, and RenderStrict stays usable on real payloads.
func leftovers(tpl string, bound map[string]Param) []string {
	matches := placeholderRegex.FindAllString(tpl, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	var order []string
	for _, m := range matches {
		key := m[1 : len(m)-1] // strip { }
		if _, isBound := bound[key]; isBound {
			continue
		}
		if !seen[m] {
			seen[m] = true
			order = append(order, m)
		}
	}
	// FindAllString scans left to right, so order is already first-occurrence.
	return order
}
