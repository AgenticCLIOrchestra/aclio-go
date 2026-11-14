package claudeevent

import (
	"encoding/json"
	"errors"
	"fmt"
)

// TypeUser is the stream line type of a user event. In a stream these carry
// the results of the model's tool calls back to it.
const TypeUser = "user"

// BlockToolResult is the content block type of a tool call's result in a user
// message. Untyped, so it compares directly with UserContentBlock.Type.
const BlockToolResult = "tool_result"

// UserEvent is one user line of the stream.
type UserEvent struct {
	Message UserMessage `json:"message"`
}

// UserMessage is the message inside a UserEvent.
type UserMessage struct {
	Role    string             `json:"role"`
	Content []UserContentBlock `json:"content"`
}

// UserContentBlock is one block of a UserMessage. For BlockToolResult,
// ToolUseID names the tool call, Content is its output and IsError is set
// when the call failed or was refused.
type UserContentBlock struct {
	Type      string `json:"type"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error"`
	ToolUseID string `json:"tool_use_id"`
}

// ErrNotUser is returned by ParseUser for a line whose type isn't "user".
var ErrNotUser = errors.New("not a user event")

// ParseUser decodes one stream line holding a user event. It fails on
// malformed JSON, on a message that doesn't match UserMessage, or on a line of
// another type.
func ParseUser(raw []byte) (UserEvent, error) {
	var event struct {
		Type string `json:"type"`
		UserEvent
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return UserEvent{}, fmt.Errorf("parse user event: %w", err)
	}
	if event.Type != TypeUser {
		return UserEvent{}, fmt.Errorf("parse user event: %w (type %q)", ErrNotUser, event.Type)
	}
	return event.UserEvent, nil
}
