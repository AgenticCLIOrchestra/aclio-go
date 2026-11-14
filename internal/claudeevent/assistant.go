package claudeevent

import (
	"encoding/json"
	"errors"
	"fmt"
)

// TypeAssistant is the stream line type of an assistant event: one message
// from the model.
const TypeAssistant = "assistant"

// The content block types seen in an assistant message. Untyped constants, so
// they compare directly with ContentBlock.Type.
const (
	BlockThinking = "thinking"
	BlockText     = "text"
	BlockToolUse  = "tool_use"
)

// AssistantEvent is one assistant line of the stream.
type AssistantEvent struct {
	Message AssistantMessage `json:"message"`
}

// AssistantMessage is the model's message inside an AssistantEvent.
type AssistantMessage struct {
	Type    string         `json:"type"`
	Content []ContentBlock `json:"content"`
}

// ContentBlock is one block of an AssistantMessage. Which fields are set
// depends on Type: Thinking for BlockThinking, Text for BlockText, Name and
// Input for BlockToolUse. Blocks of other types are kept, with only Type set.
type ContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Name     string `json:"name"`
	Input    any    `json:"input"`
	Thinking string `json:"thinking"`
}

// ErrNotAssistant is returned by ParseAssistant for a line whose type isn't
// "assistant".
var ErrNotAssistant = errors.New("not an assistant event")

// ParseAssistant decodes one stream line holding an assistant event. It fails
// on malformed JSON, on a message that doesn't match AssistantMessage, or on a
// line of another type.
func ParseAssistant(raw []byte) (AssistantEvent, error) {
	var event struct {
		Type string `json:"type"`
		AssistantEvent
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return AssistantEvent{}, fmt.Errorf("parse assistant event: %w", err)
	}
	if event.Type != TypeAssistant {
		return AssistantEvent{}, fmt.Errorf("parse assistant event: %w (type %q)", ErrNotAssistant, event.Type)
	}
	return event.AssistantEvent, nil
}
