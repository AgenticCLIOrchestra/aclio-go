package claudeevent

import (
	"errors"
	"reflect"
	"testing"
)

func TestParseAssistant(t *testing.T) {
	tests := []struct {
		name string
		line string
		want AssistantEvent
	}{
		{
			"every block type",
			`{"type":"assistant","session_id":"s-1","message":{"type":"message","content":[` +
				`{"type":"thinking","thinking":"plan"},` +
				`{"type":"text","text":"hello"},` +
				`{"type":"tool_use","name":"Read","input":{"file_path":"/a.go"}},` +
				`{"type":"something_new","whatever":1}]}}`,
			AssistantEvent{Message: AssistantMessage{
				Type: "message",
				Content: []ContentBlock{
					{Type: BlockThinking, Thinking: "plan"},
					{Type: BlockText, Text: "hello"},
					{Type: BlockToolUse, Name: "Read", Input: map[string]any{"file_path": "/a.go"}},
					{Type: "something_new"},
				},
			}},
		},
		{
			"no message",
			`{"type":"assistant"}`,
			AssistantEvent{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAssistant([]byte(tt.line))
			if err != nil {
				t.Fatalf("ParseAssistant: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseAssistant =\n %#v\nwant\n %#v", got, tt.want)
			}
		})
	}
}

func TestParseAssistantErrors(t *testing.T) {
	tests := []struct {
		name         string
		line         string
		notAssistant bool
	}{
		{"malformed JSON", `{"type":"assistant"`, false},
		{"message of the wrong shape", `{"type":"assistant","message":"hi"}`, false},
		{"content of the wrong shape", `{"type":"assistant","message":{"content":{}}}`, false},
		{"another type", `{"type":"system","message":{}}`, true},
		{"no type", `{"message":{}}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAssistant([]byte(tt.line))
			if err == nil {
				t.Fatalf("ParseAssistant = %#v, want an error", got)
			}
			if errors.Is(err, ErrNotAssistant) != tt.notAssistant {
				t.Errorf("errors.Is(err, ErrNotAssistant) = %v, want %v (err: %v)", !tt.notAssistant, tt.notAssistant, err)
			}
		})
	}
}
