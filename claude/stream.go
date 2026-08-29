package claude

import (
	"encoding/json"
	"errors"
	"os/exec"
	"strings"

	"github.com/agenticcliorchestra/aclio-go/internal/cliexec"
)

// runStream drives cmd in streaming mode and returns the CLI's result blob (the
// last "result" line). Every other event is logged to cliexec.LogWriter under a
// uniform, greppable taxonomy — one line each, all led by prefix ("[claude]",
// or "[claude] [name]" for a named run): [thinking] / [text] (trimmed,
// truncated), [tool] [<Name>] <summary>, [tool] [denied] <content> for a
// rejected tool call, [system] (init collapsed to a marker, other subtypes
// passed through raw), and [<type>] / [<type>] [<sub>] for anything
// unrecognised. Malformed lines are skipped silently.
func runStream(cmd *exec.Cmd, prefix string) (string, error) {
	var lastResultLine string

	err := cliexec.Stream(cmd, func(line string) {
		handleStreamLine(line, &lastResultLine, prefix)
	})
	if err != nil {
		return "", err
	}

	if lastResultLine == "" {
		return "", errors.New("no result event received from stream")
	}
	return lastResultLine, nil
}

// handleStreamLine parses one stream-json line, logs it under the taxonomy led
// by prefix, and captures the CLI's result blob into *lastResult. A malformed
// line is skipped silently so the stream continues. Split out from runStream so
// the rendering contract can be tested without spawning a process.
func handleStreamLine(line string, lastResult *string, prefix string) {
	var event streamEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return
	}
	switch event.Type {
	case "assistant":
		printAssistantEvent(event, prefix)
	case "result":
		*lastResult = line
	case "user":
		printUserEvent(event, prefix)
	case "system":
		printSystemEvent(event, line, prefix)
	default:
		// Unrecognised event: keep the subtype when the CLI provides one, so a
		// future event type is at least self-describing in the logs.
		if event.Subtype != "" {
			cliexec.Logf("%s [%s] [%s]", prefix, event.Type, event.Subtype)
		} else {
			cliexec.Logf("%s [%s]", prefix, event.Type)
		}
	}
}

type streamEvent struct {
	Type    string          `json:"type"`
	Subtype string          `json:"subtype"`
	Message json.RawMessage `json:"message"`
}

// printSystemEvent logs a system event. The init handshake is pure noise, so
// it collapses to a marker; anything else (e.g. rate_limit_event) is dumped
// raw so nothing is silently lost.
func printSystemEvent(event streamEvent, raw, prefix string) {
	if event.Subtype == "init" {
		cliexec.Logf("%s [system] init", prefix)
		return
	}
	cliexec.Logf("%s [system] %s", prefix, raw)
}

type userMessage struct {
	Role    string `json:"role"`
	Content []struct {
		Type      string `json:"type"`
		Content   string `json:"content"`
		IsError   bool   `json:"is_error"`
		ToolUseId string `json:"tool_use_id"`
	} `json:"content"`
}

func printUserEvent(event streamEvent, prefix string) {
	var msg userMessage
	if err := json.Unmarshal(event.Message, &msg); err != nil {
		return
	}
	for _, block := range msg.Content {
		if block.Type == "tool_result" && block.IsError {
			// Truncate: a denied Bash call can carry kilobytes of output, and one
			// screen-flooding line breaks the scannable-stream contract.
			cliexec.Logf("%s [tool] [denied] %s", prefix, cliexec.Truncate(strings.TrimSpace(block.Content), 200))
		}
	}
}

type assistantMessage struct {
	Type    string `json:"type"`
	Content []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Name     string `json:"name"`
		Input    any    `json:"input"`
		Thinking string `json:"thinking"`
	} `json:"content"`
}

func printAssistantEvent(event streamEvent, prefix string) {
	var msg assistantMessage
	if err := json.Unmarshal(event.Message, &msg); err != nil {
		return
	}
	for _, block := range msg.Content {
		switch block.Type {
		case "thinking":
			trimmed := strings.TrimSpace(block.Thinking)
			if trimmed != "" {
				cliexec.Logf("%s [thinking] %s", prefix, cliexec.Truncate(trimmed, 200))
			}
		case "text":
			trimmed := strings.TrimSpace(block.Text)
			if trimmed != "" {
				cliexec.Logf("%s [text] %s", prefix, cliexec.Truncate(trimmed, 200))
			}
		case "tool_use":
			if summary := formatToolInput(block.Name, block.Input); summary != "" {
				cliexec.Logf("%s [tool] [%s] %s", prefix, block.Name, summary)
				continue
			}
			text, err := json.Marshal(block.Input)
			if err != nil {
				continue
			}
			cliexec.Logf("%s [tool] [%s] %s", prefix, block.Name, cliexec.Truncate(string(text), 200))
		}
	}
}

// formatToolInput renders a one-line summary for the common built-in tools. It
// returns "" both for unknown tools and for a known tool whose expected field
// is absent; either way the caller falls back to the raw (truncated) JSON
// input. That fallback is deliberate — a known tool with a genuinely empty
// input renders honestly as e.g. `[claude] [tool] [Bash] {}` rather than
// pretending there was a summary.
func formatToolInput(name string, input any) string {
	m, ok := input.(map[string]any)
	if !ok {
		return ""
	}
	switch name {
	case "Grep":
		pattern, _ := m["pattern"].(string)
		path, _ := m["path"].(string)
		if path != "" {
			return cliexec.Truncate(pattern+" in "+path, 150)
		}
		return cliexec.Truncate(pattern, 150)
	case "Glob":
		pattern, _ := m["pattern"].(string)
		return cliexec.Truncate(pattern, 150)
	case "Read", "Edit", "Write":
		path, _ := m["file_path"].(string)
		return cliexec.Truncate(path, 150)
	case "Bash":
		cmd, _ := m["command"].(string)
		return cliexec.Truncate(cmd, 150)
	default:
		return ""
	}
}
