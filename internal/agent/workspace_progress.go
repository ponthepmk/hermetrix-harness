package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	toolruntime "hermetrix-harness/internal/tools"
)

// Tool-call-looking prose is not a native invocation and must not be recorded
// as a completed answer. Fenced examples and inline explanations remain text.
func hasUnexecutedToolMarkup(content string) bool {
	fence := ""
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			marker := line[:3]
			if fence == "" {
				fence = marker
			} else if fence == marker {
				fence = ""
			}
			continue
		}
		if fence == "" && (strings.HasPrefix(line, "<tool_call>") || strings.HasPrefix(line, "<function=")) {
			return true
		}
	}
	return false
}

func workspaceProgressPrompt(events []Event, turnID string) string {
	reads := 0
	for _, event := range events {
		if event.TurnID != turnID || event.EventKind != "tool_result" {
			continue
		}
		var receipt toolruntime.Receipt
		if json.Unmarshal([]byte(event.Content), &receipt) != nil {
			continue
		}
		if receipt.Effect != "read" {
			reads = 0
			continue
		}
		switch receipt.Name {
		case "workspace.read_file", "workspace.search_files", "workspace.list_files", "context_search":
			reads++
		}
	}
	if reads < 4 {
		return ""
	}
	return fmt.Sprintf("This turn has %d consecutive workspace/evidence reads with no effect attempt. If the user requested implementation, use the evidence already collected to make the next scoped change once sufficient; otherwise identify the exact missing fact. Avoid another broad read of unchanged source. Read-only requests may continue inspecting. Do not invent a write, skip verification or widen tool authority.", reads)
}
