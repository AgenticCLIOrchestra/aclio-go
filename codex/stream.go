package codex

import (
	"encoding/json"
	"os/exec"
	"strings"

	"github.com/agenticcliorchestra/aclio-go/internal/cliexec"
)

// codexEvent is one line of `codex exec --json` output. Codex emits a small
// set of top-level event types; item.completed carries the actual work items.
type codexEvent struct {
	Type     string          `json:"type"`
	ThreadID string          `json:"thread_id"`
	Item     json.RawMessage `json:"item"`
	Usage    *Usage          `json:"usage"`
}

// codexItem is the payload of an item.* event. Only the fields we log or
// return are decoded; the shape is otherwise left loose.
type codexItem struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Text    string `json:"text"`
	Command string `json:"command"`
}

// runStream drives cmd, parsing the JSONL event stream into a RunResult and
// (when logEvents) logging each event as it arrives, led by the logger's prefix ("[codex]"
// or "[codex] [name]"). The final agent_message item's text becomes
// RunResult.FinalText.
func runStream(cmd *exec.Cmd, logEvents bool, logger cliexec.Logger) (*RunResult, error) {
	result := &RunResult{}

	err := cliexec.Stream(cmd, func(line string) {
		handleStreamLine(line, result, logEvents, logger)
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// handleStreamLine parses one JSONL line, records what the run returns
// (thread id, usage, final message) into result, and, when logEvents, renders
// the line and emits it through the run's handler. The result is recorded
// before the handler sees anything, so the handler can't affect it. A
// malformed line, or an item.completed whose item is malformed, is skipped
// silently and never reaches the handler. Split out from runStream so it can
// be tested without spawning a process.
func handleStreamLine(line string, result *RunResult, logEvents bool, logger cliexec.Logger) {
	var event codexEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return
	}

	var item codexItem
	if strings.HasPrefix(event.Type, "item.") {
		if err := json.Unmarshal(event.Item, &item); err != nil && event.Type == "item.completed" {
			return
		}
	}

	switch event.Type {
	case "thread.started":
		result.ThreadID = event.ThreadID
	case "turn.completed":
		if event.Usage != nil {
			result.Usage = *event.Usage
		}
	case "item.completed":
		if item.Type == "agent_message" {
			// The last agent message is the agent's final answer.
			result.FinalText = item.Text
		}
	}

	if !logEvents {
		return
	}
	cliexec.EmitStream(logger, event.Type, item.Type, line, nil, renderEvent(event, item, line, logger))
}

// renderEvent renders the log lines for one event.
func renderEvent(event codexEvent, item codexItem, line string, logger cliexec.Logger) []string {
	switch event.Type {
	case "thread.started":
		return []string{cliexec.Line(logger, "[thread] %s", event.ThreadID)}
	case "turn.completed":
		return nil
	case "item.completed":
		return renderItem(item, logger)
	case "error":
		return []string{cliexec.Line(logger, "[error] %s", cliexec.Truncate(line, 200))}
	default:
		return []string{cliexec.Line(logger, "[%s]", event.Type)}
	}
}

// renderItem renders the log lines for a completed item.
func renderItem(item codexItem, logger cliexec.Logger) []string {
	switch item.Type {
	case "agent_message", "reasoning":
		trimmed := strings.TrimSpace(item.Text)
		if trimmed == "" {
			return nil
		}
		tag := "[text]"
		if item.Type == "reasoning" {
			tag = "[reasoning]"
		}
		return []string{cliexec.Line(logger, "%s %s", tag, cliexec.Truncate(trimmed, 200))}
	case "command_execution":
		return []string{cliexec.Line(logger, "[command] %s", cliexec.Truncate(item.Command, 150))}
	default:
		return []string{cliexec.Line(logger, "[item] [%s]", item.Type)}
	}
}
