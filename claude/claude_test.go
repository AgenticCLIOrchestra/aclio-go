package claude

import "testing"

func TestIsValidModel(t *testing.T) {
	valid := []Model{
		Default, Opus, Sonnet, Haiku, OpusPlan,
		Fable5, Opus48, Sonnet5, Haiku45,
		"claude-sonnet-4-6", "claude-opus-4-8[1m]", "claude-2.1",
	}
	for _, m := range valid {
		if !IsValidModel(m) {
			t.Errorf("IsValidModel(%q) = false, want true", m)
		}
	}

	invalid := []Model{
		"", "gpt-4", "claude-", "claude-opus; rm -rf /", "--model", "claude-opus 4",
	}
	for _, m := range invalid {
		if IsValidModel(m) {
			t.Errorf("IsValidModel(%q) = true, want false", m)
		}
	}
}

func TestIsValidAllowedTool(t *testing.T) {
	valid := []string{
		"Bash", "Read", "WebFetch", "Bash(git status)", "WebFetch(domain:example.com)",
		"mcp__github", "mcp__github__get_issue",
	}
	for _, tool := range valid {
		if !isValidAllowedTool(tool) {
			t.Errorf("isValidAllowedTool(%q) = false, want true", tool)
		}
	}

	invalid := []string{"", "--allowedTools", "Bash git", "1Bash", "-Bash"}
	for _, tool := range invalid {
		if isValidAllowedTool(tool) {
			t.Errorf("isValidAllowedTool(%q) = true, want false", tool)
		}
	}
}

func TestParseResultAndStructured(t *testing.T) {
	blob := `{
		"type": "result",
		"is_error": false,
		"session_id": "sess-123",
		"total_cost_usd": 0.0123,
		"structured_output": {"answer": "42", "confidence": 0.9},
		"usage": {
			"input_tokens": 10,
			"output_tokens": 20,
			"cache_read_input_tokens": 30,
			"cache_creation_input_tokens": 40,
			"cache_creation": {"ephemeral_1h_input_tokens": 5, "ephemeral_5m_input_tokens": 35}
		}
	}`

	parsed, err := ParseResult(blob)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.SessionID != "sess-123" {
		t.Errorf("SessionID = %q", parsed.SessionID)
	}
	if parsed.Usage.CacheReadInputTokens != 30 {
		t.Errorf("CacheReadInputTokens = %d", parsed.Usage.CacheReadInputTokens)
	}
	if parsed.Usage.CacheCreation.Ephemeral5mInputTokens != 35 {
		t.Errorf("Ephemeral5mInputTokens = %d", parsed.Usage.CacheCreation.Ephemeral5mInputTokens)
	}

	var out struct {
		Answer     string  `json:"answer"`
		Confidence float64 `json:"confidence"`
	}
	if err := Structured(blob, &out); err != nil {
		t.Fatal(err)
	}
	if out.Answer != "42" || out.Confidence != 0.9 {
		t.Errorf("Structured = %+v", out)
	}
}

func TestParseStructured(t *testing.T) {
	blob := `{
		"type": "result",
		"session_id": "sess-456",
		"structured_output": {"answer": "42"}
	}`

	type out struct {
		Answer string `json:"answer"`
	}
	decoded, parsed, err := ParseStructured[out](blob)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Answer != "42" {
		t.Errorf("decoded = %+v", decoded)
	}
	if parsed.SessionID != "sess-456" {
		t.Errorf("SessionID = %q", parsed.SessionID)
	}

	if _, _, err := ParseStructured[out]("not json"); err == nil {
		t.Error("ParseStructured accepted a non-JSON blob")
	}
}

func TestRunRejectsInvalidInputs(t *testing.T) {
	if _, err := Run(t.TempDir(), RunOpts{Prompt: "hi", ModelID: "gpt-4"}); err == nil {
		t.Error("Run accepted an invalid model")
	}
	if _, err := Run(t.TempDir(), RunOpts{Prompt: "hi", ModelID: Sonnet, AllowedTools: []string{"--bad"}}); err == nil {
		t.Error("Run accepted an invalid allowed tool")
	}
}
