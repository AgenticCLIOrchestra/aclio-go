package codex

// RunResult is the outcome of a `codex exec` invocation, assembled from the
// JSONL event stream.
type RunResult struct {
	// ThreadID is Codex's session id (from thread.started); pass it back as
	// RunOpts.ResumeSessionID to continue the conversation.
	ThreadID string `json:"thread_id"`
	// FinalText is the agent's last message — the final answer, or the JSON
	// document when an output schema was supplied.
	FinalText string `json:"final_text"`
	// Usage is the token accounting from turn.completed.
	Usage Usage `json:"usage"`
}

// Usage mirrors the `usage` block on Codex's turn.completed event.
type Usage struct {
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
}
