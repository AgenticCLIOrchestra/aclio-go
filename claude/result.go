package claude

import (
	"encoding/json"
	"fmt"
)

// RunResult mirrors the claude CLI's result blob (--output-format json, or
// the final stream-json event).
type RunResult struct {
	Type             string         `json:"type"`
	Subtype          string         `json:"subtype"`
	IsError          bool           `json:"is_error"`
	DurationMs       int            `json:"duration_ms"`
	DurationApiMs    int            `json:"duration_api_ms"`
	NumTurns         int            `json:"num_turns"`
	Result           string         `json:"result"`
	StopReason       *string        `json:"stop_reason"`
	SessionID        string         `json:"session_id"`
	TotalCostUSD     float64        `json:"total_cost_usd"`
	StructuredOutput map[string]any `json:"structured_output"`
	Usage            Usage          `json:"usage"`
	UUID             string         `json:"uuid"`
}

// Usage mirrors the relevant fields from the CLI's `usage` block in the
// `result` event. The CLI populates `cache_creation` with the per-TTL-tier
// split so we can see whether the prompt prefix landed in the 1h or 5m tier.
type Usage struct {
	InputTokens              int           `json:"input_tokens"`
	OutputTokens             int           `json:"output_tokens"`
	CacheReadInputTokens     int           `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int           `json:"cache_creation_input_tokens"`
	CacheCreation            CacheCreation `json:"cache_creation"`
}

type CacheCreation struct {
	Ephemeral1hInputTokens int `json:"ephemeral_1h_input_tokens"`
	Ephemeral5mInputTokens int `json:"ephemeral_5m_input_tokens"`
}

// ParseResult decodes a claude CLI result blob.
func ParseResult(s string) (*RunResult, error) {
	var result RunResult
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Structured decodes the structured_output of a claude CLI result blob into
// dst.
func Structured(cliOutput string, dst any) error {
	parsed, err := ParseResult(cliOutput)
	if err != nil {
		return fmt.Errorf("parsing claude output: %w", err)
	}
	raw, err := json.Marshal(parsed.StructuredOutput)
	if err != nil {
		return fmt.Errorf("marshalling structured output: %w", err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("unmarshalling structured output: %w", err)
	}
	return nil
}
