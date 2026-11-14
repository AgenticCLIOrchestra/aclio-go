package claudeevent

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

// Fixtures are trimmed copies of real CLI lines with made-up IDs and paths.
const (
	initLine = `{"type":"system","subtype":"init","cwd":"/work/agent","session_id":"s-1","uuid":"u-1",` +
		`"tools":["Read","StructuredOutput"],` +
		`"mcp_servers":[{"name":"example","status":"needs-auth","source":"claudeai"}],` +
		`"model":"claude-opus-5-5","permissionMode":"default","apiKeySource":"none",` +
		`"claude_code_version":"2.1.325","agents":["Explore"],"skills":["simplify"],"slash_commands":["clear"],` +
		`"plugins":[{"name":"p","path":"builtin","source":"p@builtin"},{"name":"q","path":"/plugins/q","source":"q@market","version":"1.0.0"}],` +
		`"capabilities":["something_v1"],"view_mode":"default"}`

	permissionDeniedLine = `{"type":"system","subtype":"permission_denied","tool_name":"Bash","tool_use_id":"toolu_1",` +
		`"decision_reason_type":"other","decision_reason":"A variable can't be checked",` +
		`"message":"A variable can't be checked","uuid":"u-2","session_id":"s-1"}`

	taskStartedLine = `{"type":"system","subtype":"task_started","task_id":"t-1","run_id":"r-1","tool_use_id":"toolu_2",` +
		`"description":"npm run build","is_backgrounded":true,"task_type":"local_bash","uuid":"u-3","session_id":"s-1"}`

	taskNotificationLine = `{"type":"system","subtype":"task_notification","task_id":"t-1","run_id":"r-1","tool_use_id":"toolu_2",` +
		`"status":"completed","output_file":"","summary":"npm run build","uuid":"u-4","session_id":"s-1"}`

	thinkingTokensLine = `{"type":"system","subtype":"thinking_tokens","estimated_tokens":300,"estimated_tokens_delta":100,` +
		`"session_id":"s-1","uuid":"u-5"}`

	unknownLine = `{"type":"system","subtype":"rate_limit_event","session_id":"s-1","uuid":"u-6","extra":1}`
)

func TestParseSystem(t *testing.T) {
	tests := []struct {
		name string
		line string
		want System
	}{
		{
			"init keeps only the typed fields",
			initLine,
			InitEvent{
				Base:              Base{Subtype: SubtypeInit, SessionID: "s-1", UUID: "u-1"},
				CWD:               "/work/agent",
				Model:             "claude-opus-5-5",
				PermissionMode:    "default",
				APIKeySource:      "none",
				ClaudeCodeVersion: "2.1.325",
				Tools:             []string{"Read", "StructuredOutput"},
				MCPServers:        []MCPServer{{Name: "example", Status: "needs-auth", Source: "claudeai"}},
				Agents:            []string{"Explore"},
				Skills:            []string{"simplify"},
				SlashCommands:     []string{"clear"},
				Plugins: []Plugin{
					{Name: "p", Path: "builtin", Source: "p@builtin"},
					{Name: "q", Path: "/plugins/q", Source: "q@market", Version: "1.0.0"},
				},
			},
		},
		{
			"permission denied",
			permissionDeniedLine,
			PermissionDeniedEvent{
				Base:               Base{Subtype: SubtypePermissionDenied, SessionID: "s-1", UUID: "u-2"},
				ToolName:           "Bash",
				ToolUseID:          "toolu_1",
				DecisionReasonType: "other",
				DecisionReason:     "A variable can't be checked",
				Message:            "A variable can't be checked",
			},
		},
		{
			"task started",
			taskStartedLine,
			TaskStartedEvent{
				Base:           Base{Subtype: SubtypeTaskStarted, SessionID: "s-1", UUID: "u-3"},
				TaskID:         "t-1",
				RunID:          "r-1",
				ToolUseID:      "toolu_2",
				Description:    "npm run build",
				IsBackgrounded: true,
				TaskType:       "local_bash",
			},
		},
		{
			"task notification",
			taskNotificationLine,
			TaskNotificationEvent{
				Base:      Base{Subtype: SubtypeTaskNotification, SessionID: "s-1", UUID: "u-4"},
				TaskID:    "t-1",
				RunID:     "r-1",
				ToolUseID: "toolu_2",
				Status:    "completed",
				Summary:   "npm run build",
			},
		},
		{
			"thinking tokens",
			thinkingTokensLine,
			ThinkingTokensEvent{
				Base:                 Base{Subtype: SubtypeThinkingTokens, SessionID: "s-1", UUID: "u-5"},
				EstimatedTokens:      300,
				EstimatedTokensDelta: 100,
			},
		},
		{
			"unknown subtype is not an error and keeps the whole line",
			unknownLine,
			UnknownEvent{
				Base: Base{Subtype: "rate_limit_event", SessionID: "s-1", UUID: "u-6"},
				Raw:  json.RawMessage(unknownLine),
			},
		},
		{
			"missing subtype is unknown",
			`{"type":"system"}`,
			UnknownEvent{Raw: json.RawMessage(`{"type":"system"}`)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSystem([]byte(tt.line))
			if err != nil {
				t.Fatalf("ParseSystem: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseSystem =\n %#v\nwant\n %#v", got, tt.want)
			}
		})
	}
}

func TestParseSystemErrors(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		notSystem bool
	}{
		{"malformed JSON", `{"type":"system"`, false},
		{"not an object", `[]`, false},
		{"wrong field type", `{"type":"system","subtype":"thinking_tokens","estimated_tokens":"lots"}`, false},
		{"another type", `{"type":"assistant","subtype":"init"}`, true},
		{"no type", `{"subtype":"init"}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSystem([]byte(tt.line))
			if err == nil {
				t.Fatalf("ParseSystem = %#v, want an error", got)
			}
			if errors.Is(err, ErrNotSystem) != tt.notSystem {
				t.Errorf("errors.Is(err, ErrNotSystem) = %v, want %v (err: %v)", !tt.notSystem, tt.notSystem, err)
			}
		})
	}
}
