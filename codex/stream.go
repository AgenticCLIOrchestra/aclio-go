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

// codexItem is the payload of an item.completed event. Only the fields we log
// or return are decoded; the shape is otherwise left loose.
type codexItem struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Text    string `json:"text"`
	Command string `json:"command"`
}

// runStream drives cmd, parsing the JSONL event stream into a RunResult and
// (when logEvents) logging each event as it arrives. The final agent_message
// item's text becomes RunResult.FinalText.
func runStream(cmd *exec.Cmd, logEvents bool) (*RunResult, error) {
	result := &RunResult{}

	err := cliexec.Stream(cmd, func(line string) {
		var event codexEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return
		}

		switch event.Type {
		case "thread.started":
			result.ThreadID = event.ThreadID
			if logEvents {
				cliexec.Logf("[codex] [thread] %s", event.ThreadID)
			}
		case "turn.completed":
			if event.Usage != nil {
				result.Usage = *event.Usage
			}
		case "item.completed":
			var item codexItem
			if err := json.Unmarshal(event.Item, &item); err != nil {
				return
			}
			handleItem(result, item, logEvents)
		case "error":
			if logEvents {
				cliexec.Logf("[codex] [error] %s", cliexec.Truncate(line, 200))
			}
		default:
			if logEvents {
				cliexec.Logf("[codex] [%s]", event.Type)
			}
		}
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// handleItem records the final agent message and logs items of interest.
func handleItem(result *RunResult, item codexItem, logEvents bool) {
	switch item.Type {
	case "agent_message":
		// The last agent message is the agent's final answer.
		result.FinalText = item.Text
		if logEvents {
			trimmed := strings.TrimSpace(item.Text)
			if trimmed != "" {
				cliexec.Logf("[codex] [text] %s", cliexec.Truncate(trimmed, 200))
			}
		}
	case "reasoning":
		if logEvents {
			trimmed := strings.TrimSpace(item.Text)
			if trimmed != "" {
				cliexec.Logf("[codex] [reasoning] %s", cliexec.Truncate(trimmed, 200))
			}
		}
	case "command_execution":
		if logEvents {
			cliexec.Logf("[codex] [command] %s", cliexec.Truncate(item.Command, 150))
		}
	default:
		if logEvents {
			cliexec.Logf("[codex] [item] [%s]", item.Type)
		}
	}
}
