// Package claudeevent types the events in claude's stream-json output. For now
// it covers the "assistant", "user" and "system" events. Each system
// subtype seen so far has its own struct, and any other subtype decodes to
// UnknownEvent rather than failing, because the CLI adds subtypes without
// notice.
//
// String fields whose values form a set (a task's status, an MCP server's
// status, the permission mode, …) stay plain strings: only one or two values
// of each have been seen, too few to define their full set.
package claudeevent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// TypeSystem is the stream line type of every system event.
const TypeSystem = "system"

// The system event subtypes seen so far. They are untyped constants, so they
// compare directly with a plain string subtype.
const (
	SubtypeInit             = "init"
	SubtypePermissionDenied = "permission_denied"
	SubtypeTaskStarted      = "task_started"
	SubtypeTaskNotification = "task_notification"
	SubtypeThinkingTokens   = "thinking_tokens"
)

// Base holds the fields every system event carries.
type Base struct {
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
	UUID      string `json:"uuid"`
}

// InitEvent is the handshake the CLI sends first. Only the fields useful for
// diagnosing a run are typed; the rest of the line (internal flags,
// capabilities, socket paths, …) changes too often to depend on.
type InitEvent struct {
	Base
	CWD               string      `json:"cwd"`
	Model             string      `json:"model"`
	PermissionMode    string      `json:"permissionMode"` // camelCase in the CLI's output, unlike most fields
	APIKeySource      string      `json:"apiKeySource"`   // likewise
	ClaudeCodeVersion string      `json:"claude_code_version"`
	Tools             []string    `json:"tools"`
	MCPServers        []MCPServer `json:"mcp_servers"`
	Agents            []string    `json:"agents"`
	Skills            []string    `json:"skills"`
	SlashCommands     []string    `json:"slash_commands"`
	Plugins           []Plugin    `json:"plugins"`
}

// MCPServer is one MCP server listed in InitEvent.
type MCPServer struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Source string `json:"source"`
}

// Plugin is one plugin listed in InitEvent. Version is empty for built-ins.
type Plugin struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Source  string `json:"source"`
	Version string `json:"version"`
}

// PermissionDeniedEvent reports a tool call the CLI refused to run.
type PermissionDeniedEvent struct {
	Base
	ToolName           string `json:"tool_name"`
	ToolUseID          string `json:"tool_use_id"`
	DecisionReasonType string `json:"decision_reason_type"`
	DecisionReason     string `json:"decision_reason"`
	Message            string `json:"message"`
}

// TaskStartedEvent reports a task (e.g. a Bash command) starting.
type TaskStartedEvent struct {
	Base
	TaskID         string `json:"task_id"`
	RunID          string `json:"run_id"`
	ToolUseID      string `json:"tool_use_id"`
	Description    string `json:"description"`
	IsBackgrounded bool   `json:"is_backgrounded"`
	TaskType       string `json:"task_type"`
}

// TaskNotificationEvent reports a change in a task's state, such as it
// completing.
type TaskNotificationEvent struct {
	Base
	TaskID     string `json:"task_id"`
	RunID      string `json:"run_id"`
	ToolUseID  string `json:"tool_use_id"`
	Status     string `json:"status"`
	OutputFile string `json:"output_file"`
	Summary    string `json:"summary"`
}

// ThinkingTokensEvent is a progress tick while the model thinks.
// EstimatedTokens is the running total, EstimatedTokensDelta the growth since
// the previous tick.
type ThinkingTokensEvent struct {
	Base
	EstimatedTokens      int `json:"estimated_tokens"`
	EstimatedTokensDelta int `json:"estimated_tokens_delta"`
}

// UnknownEvent is a system event whose subtype has no struct of its own: one
// not seen yet, or one added to the CLI later. Raw is a copy of the whole line,
// so none of its fields are lost.
type UnknownEvent struct {
	Base
	Raw json.RawMessage
}

// System is one of InitEvent, PermissionDeniedEvent, TaskStartedEvent,
// TaskNotificationEvent, ThinkingTokensEvent or UnknownEvent.
type System interface {
	isSystem()
}

func (InitEvent) isSystem()             {}
func (PermissionDeniedEvent) isSystem() {}
func (TaskStartedEvent) isSystem()      {}
func (TaskNotificationEvent) isSystem() {}
func (ThinkingTokensEvent) isSystem()   {}
func (UnknownEvent) isSystem()          {}

// ErrNotSystem is returned by ParseSystem for a line whose type isn't
// "system".
var ErrNotSystem = errors.New("not a system event")

// ParseSystem decodes one stream line holding a system event. It fails only on
// malformed JSON or a line of another type; an unrecognised subtype gives
// UnknownEvent.
func ParseSystem(raw []byte) (System, error) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, fmt.Errorf("parse system event: %w", err)
	}
	if head.Type != TypeSystem {
		return nil, fmt.Errorf("parse system event: %w (type %q)", ErrNotSystem, head.Type)
	}

	var base Base
	if err := json.Unmarshal(raw, &base); err != nil {
		return nil, fmt.Errorf("parse system event: %w", err)
	}
	switch base.Subtype {
	case SubtypeInit:
		return decode[InitEvent](raw)
	case SubtypePermissionDenied:
		return decode[PermissionDeniedEvent](raw)
	case SubtypeTaskStarted:
		return decode[TaskStartedEvent](raw)
	case SubtypeTaskNotification:
		return decode[TaskNotificationEvent](raw)
	case SubtypeThinkingTokens:
		return decode[ThinkingTokensEvent](raw)
	default:
		return UnknownEvent{Base: base, Raw: bytes.Clone(raw)}, nil
	}
}

func decode[T System](raw []byte) (System, error) {
	var event T
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, fmt.Errorf("parse system event: %w", err)
	}
	return event, nil
}
