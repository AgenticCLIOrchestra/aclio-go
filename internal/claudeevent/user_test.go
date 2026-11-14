package claudeevent

import (
	"errors"
	"reflect"
	"testing"
)

func TestParseUser(t *testing.T) {
	tests := []struct {
		name string
		line string
		want UserEvent
	}{
		{
			"tool results",
			`{"type":"user","session_id":"s-1","message":{"role":"user","content":[` +
				`{"type":"tool_result","tool_use_id":"toolu_1","content":"ok","is_error":false},` +
				`{"type":"tool_result","tool_use_id":"toolu_2","content":"blocked","is_error":true},` +
				`{"type":"something_new","whatever":1}]}}`,
			UserEvent{Message: UserMessage{
				Role: "user",
				Content: []UserContentBlock{
					{Type: BlockToolResult, ToolUseID: "toolu_1", Content: "ok"},
					{Type: BlockToolResult, ToolUseID: "toolu_2", Content: "blocked", IsError: true},
					{Type: "something_new"},
				},
			}},
		},
		{
			"no message",
			`{"type":"user"}`,
			UserEvent{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseUser([]byte(tt.line))
			if err != nil {
				t.Fatalf("ParseUser: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseUser =\n %#v\nwant\n %#v", got, tt.want)
			}
		})
	}
}

func TestParseUserErrors(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		notUser bool
	}{
		{"malformed JSON", `{"type":"user"`, false},
		{"message of the wrong shape", `{"type":"user","message":"hi"}`, false},
		{"tool result content that isn't a string", `{"type":"user","message":{"content":[{"type":"tool_result","content":[{"type":"text","text":"hi"}]}]}}`, false},
		{"another type", `{"type":"assistant","message":{}}`, true},
		{"no type", `{"message":{}}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseUser([]byte(tt.line))
			if err == nil {
				t.Fatalf("ParseUser = %#v, want an error", got)
			}
			if errors.Is(err, ErrNotUser) != tt.notUser {
				t.Errorf("errors.Is(err, ErrNotUser) = %v, want %v (err: %v)", !tt.notUser, tt.notUser, err)
			}
		})
	}
}
