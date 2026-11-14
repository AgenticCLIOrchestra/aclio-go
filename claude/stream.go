package claude

import (
	"encoding/json"
	"errors"
	"os/exec"
	"strings"

	"github.com/agenticcliorchestra/aclio-go/internal/cliexec"
	"github.com/agenticcliorchestra/aclio-go/stream/claudeevent"
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

// handleStreamLine parses one stream-json line, captures the CLI's result blob
// into *lastResult, renders the line under the taxonomy led by the logger's
// prefix, and emits it (decoded, as stream.Event.Data) through the run's
// handler. The result is captured before the handler sees anything, so the
// handler can't affect it. A malformed line is skipped silently so the stream
// continues, and never reaches the handler. Split out from runStream so the
// rendering contract can be tested without spawning a process.
func handleStreamLine(line string, lastResult *string, logger cliexec.Logger) {
	var event streamEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return
	}
	if event.Type == "result" {
		*lastResult = line
	}
	data, lines := renderStreamLine(event, line, logger)
	cliexec.EmitStream(logger, event.Type, event.Subtype, line, data, lines)
}

type streamEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
}

// renderStreamLine decodes one parsed line into its claudeevent type (nil when
// it has none or its fields don't match) and renders the log lines for it.
func renderStreamLine(event streamEvent, line string, logger cliexec.Logger) (any, []string) {
	switch event.Type {
	case claudeevent.TypeAssistant:
		msg, err := claudeevent.ParseAssistant([]byte(line))
		if err != nil {
			return nil, nil // a message that doesn't match the expected shape logs nothing, as before
		}
		return msg, renderAssistantEvent(msg, logger)
	case "result":
		return nil, nil // never logged
	case claudeevent.TypeUser:
		msg, err := claudeevent.ParseUser([]byte(line))
		if err != nil {
			return nil, nil // a message that doesn't match the expected shape logs nothing, as before
		}
		return msg, renderUserEvent(msg, logger)
	case claudeevent.TypeSystem:
		sys, err := claudeevent.ParseSystem([]byte(line))
		if err != nil {
			// A known subtype whose fields don't match its struct: log it raw,
			// as for any other system event, rather than lose it.
			return nil, []string{cliexec.Line(logger, "[system] %s", line)}
		}
		return sys, renderSystemEvent(sys, line, logger)
	default:
		// Unrecognised event: keep the subtype when the CLI provides one, so a
		// future event type is at least self-describing in the logs.
		if event.Subtype != "" {
			return nil, []string{cliexec.Line(logger, "[%s] [%s]", event.Type, event.Subtype)}
		}
		return nil, []string{cliexec.Line(logger, "[%s]", event.Type)}
	}
}

// renderSystemEvent renders a system event. The init handshake is pure noise,
// so it collapses to a marker; anything else (e.g. rate_limit_event) is dumped
// raw so nothing is silently lost.
func renderSystemEvent(event claudeevent.System, raw string, logger cliexec.Logger) []string {
	switch event.(type) {
	case claudeevent.InitEvent:
		return []string{cliexec.Line(logger, "[system] init")}
	default:
		return []string{cliexec.Line(logger, "[system] %s", raw)}
	}
}

func renderUserEvent(event claudeevent.UserEvent, logger cliexec.Logger) []string {
	var lines []string
	for _, block := range event.Message.Content {
		if block.Type == claudeevent.BlockToolResult && block.IsError {
			// Truncate: a denied Bash call can carry kilobytes of output, and one
			// screen-flooding line breaks the scannable-stream contract.
			lines = append(lines, cliexec.Line(logger, "[tool] [denied] %s", cliexec.Truncate(strings.TrimSpace(block.Content), 200)))
		}
	}
	return lines
}

func renderAssistantEvent(event claudeevent.AssistantEvent, logger cliexec.Logger) []string {
	var lines []string
	for _, block := range event.Message.Content {
		switch block.Type {
		case claudeevent.BlockThinking:
			trimmed := strings.TrimSpace(block.Thinking)
			if trimmed != "" {
				lines = append(lines, cliexec.Line(logger, "[thinking] %s", cliexec.Truncate(trimmed, 200)))
			}
		case claudeevent.BlockText:
			trimmed := strings.TrimSpace(block.Text)
			if trimmed != "" {
				lines = append(lines, cliexec.Line(logger, "[text] %s", cliexec.Truncate(trimmed, 200)))
			}
		case claudeevent.BlockToolUse:
			if summary := formatToolInput(block.Name, block.Input); summary != "" {
				lines = append(lines, cliexec.Line(logger, "[tool] [%s] %s", block.Name, summary))
				continue
			}
			text, err := json.Marshal(block.Input)
			if err != nil {
				continue
			}
			lines = append(lines, cliexec.Line(logger, "[tool] [%s] %s", block.Name, cliexec.Truncate(string(text), 200)))
		}
	}
	return lines
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
