package claude

import (
	"encoding/json"
	"errors"
	"os/exec"
	"strings"

	"github.com/agenticcliorchestra/aclio-go/internal/claudeevent"
	"github.com/agenticcliorchestra/aclio-go/internal/cliexec"
)

// runStream drives cmd in streaming mode and returns the CLI's result blob (the
// last "result" line). Every other event is logged to logger under a
// uniform, greppable taxonomy — one line each, all led by the logger's prefix ("[claude]",
// or "[claude] [name]" for a named run): [thinking] / [text] (trimmed,
// truncated), [tool] [<Name>] <summary>, [tool] [denied] <content> for a
// rejected tool call, [system] (init collapsed to a marker, other subtypes
// passed through raw), and [<type>] / [<type>] [<sub>] for anything
// unrecognised. Malformed lines are skipped silently.
func runStream(cmd *exec.Cmd, logger cliexec.Logger) (string, error) {
	var lastResultLine string

	err := cliexec.Stream(cmd, func(line string) {
		handleStreamLine(line, &lastResultLine, logger)
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
// by the logger's prefix, and captures the CLI's result blob into *lastResult. A malformed
// line is skipped silently so the stream continues. Split out from runStream so
// the rendering contract can be tested without spawning a process.
func handleStreamLine(line string, lastResult *string, logger cliexec.Logger) {
	var event streamEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return
	}
	switch event.Type {
	case "assistant":
		printAssistantEvent(event, logger)
	case "result":
		*lastResult = line
	case "user":
		printUserEvent(event, logger)
	case claudeevent.TypeSystem:
		sys, err := claudeevent.ParseSystem([]byte(line))
		if err != nil {
			// A known subtype whose fields don't match its struct: log it raw,
			// as for any other system event, rather than lose it.
			cliexec.Log(logger, "[system] %s", line)
			return
		}
		printSystemEvent(sys, line, logger)
	default:
		// Unrecognised event: keep the subtype when the CLI provides one, so a
		// future event type is at least self-describing in the logs.
		if event.Subtype != "" {
			cliexec.Log(logger, "[%s] [%s]", event.Type, event.Subtype)
		} else {
			cliexec.Log(logger, "[%s]", event.Type)
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
func printSystemEvent(event claudeevent.System, raw string, logger cliexec.Logger) {
	switch event.(type) {
	case claudeevent.InitEvent:
		cliexec.Log(logger, "[system] init")
	default:
		cliexec.Log(logger, "[system] %s", raw)
	}
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

func printUserEvent(event streamEvent, logger cliexec.Logger) {
	var msg userMessage
	if err := json.Unmarshal(event.Message, &msg); err != nil {
		return
	}
	for _, block := range msg.Content {
		if block.Type == "tool_result" && block.IsError {
			// Truncate: a denied Bash call can carry kilobytes of output, and one
			// screen-flooding line breaks the scannable-stream contract.
			cliexec.Log(logger, "[tool] [denied] %s", cliexec.Truncate(strings.TrimSpace(block.Content), 200))
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

func printAssistantEvent(event streamEvent, logger cliexec.Logger) {
	var msg assistantMessage
	if err := json.Unmarshal(event.Message, &msg); err != nil {
		return
	}
	for _, block := range msg.Content {
		switch block.Type {
		case "thinking":
			trimmed := strings.TrimSpace(block.Thinking)
			if trimmed != "" {
				cliexec.Log(logger, "[thinking] %s", cliexec.Truncate(trimmed, 200))
			}
		case "text":
			trimmed := strings.TrimSpace(block.Text)
			if trimmed != "" {
				cliexec.Log(logger, "[text] %s", cliexec.Truncate(trimmed, 200))
			}
		case "tool_use":
			if summary := formatToolInput(block.Name, block.Input); summary != "" {
				cliexec.Log(logger, "[tool] [%s] %s", block.Name, summary)
				continue
			}
			text, err := json.Marshal(block.Input)
			if err != nil {
				continue
			}
			cliexec.Log(logger, "[tool] [%s] %s", block.Name, cliexec.Truncate(string(text), 200))
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
