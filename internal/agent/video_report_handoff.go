package agent

import (
	"encoding/json"
	"regexp"
	"strings"

	toolruntime "hermetrix-harness/internal/tools"
)

var videoReportPath = regexp.MustCompile(`^\.research-jobs/[a-f0-9]{32}/report\.md$`)

// Recognize an observed completed owner-scoped worker -> actual report read.
// This is completion guidance and evidence retention, never automatic tool
// dispatch, a fabricated answer, a budget reset or additional authority.
func completedVideoReportRead(events []Event, turnID string) (string, string) {
	goal := ""
	for _, event := range events {
		if event.TurnID == turnID && event.EventKind == "message" && event.Role == "user" {
			goal = event.Content
		}
	}
	lower := strings.ToLower(goal)
	if (!strings.Contains(lower, "youtube.com") && !strings.Contains(lower, "youtu.be")) || (!strings.Contains(lower, "สรุป") && !strings.Contains(lower, "research") && !strings.Contains(lower, "summar")) {
		return "", ""
	}
	for _, coding := range []string{"แก้โค้ด", "เขียนโค้ดให้", "modify the code", "implement the", "debug the code"} {
		if strings.Contains(lower, coding) {
			return "", ""
		}
	}
	type invocation struct {
		name string
		args map[string]any
	}
	calls := map[string]invocation{}
	ownedJobs, reports := map[string]bool{}, map[string]bool{}
	readEvent, readCall := "", ""
	for _, event := range events {
		if event.TurnID != turnID {
			continue
		}
		callID := metadataString(event.Metadata, "tool_call_id")
		if event.EventKind == "tool_call" {
			var args map[string]any
			if json.Unmarshal([]byte(event.Content), &args) == nil {
				calls[callID] = invocation{metadataString(event.Metadata, "tool_name"), args}
			}
			continue
		}
		if event.EventKind != "tool_result" {
			continue
		}
		var receipt toolruntime.Receipt
		if json.Unmarshal([]byte(event.Content), &receipt) != nil || receipt.Status != "succeeded" || receipt.ToolCallID != callID {
			continue
		}
		call, ok := calls[callID]
		if !ok || call.name != receipt.Name {
			continue
		}
		if call.name == "workspace.run" {
			action, _ := call.args["action"].(string)
			if action == "start" && call.args["executable"] == "node" {
				arguments, _ := call.args["arguments"].([]any)
				workingDir, _ := call.args["working_dir"].(string)
				if len(arguments) < 3 || arguments[0] != "cli.mjs" || (workingDir != "" && workingDir != ".") {
					continue
				}
				for index := 1; index+1 < len(arguments); index++ {
					url, _ := arguments[index+1].(string)
					if arguments[index] == "--url" && url != "" && strings.Contains(goal, url) {
						if jobID := metadataString(receipt.Metadata, "job_id"); jobID != "" {
							ownedJobs[jobID] = true
						}
					}
				}
			}
			jobID, _ := call.args["job_id"].(string)
			exit, exitPresent := receipt.Metadata["exit_code"].(float64)
			if action != "status" || jobID == "" || !ownedJobs[jobID] || metadataString(receipt.Metadata, "job_id") != jobID || receipt.Metadata["state"] != "completed" || !exitPresent || exit != 0 {
				continue
			}
			for _, line := range strings.Split(receipt.Output, "\n") {
				var terminal struct {
					Verified bool   `json:"verified_result"`
					Report   string `json:"report_file"`
				}
				if json.Unmarshal([]byte(line), &terminal) == nil && terminal.Verified {
					path := strings.ReplaceAll(terminal.Report, `\`, "/")
					if videoReportPath.MatchString(path) {
						reports[path] = true
					}
				}
			}
		}
		if call.name == "workspace.read_file" {
			path, _ := call.args["path"].(string)
			path = strings.ReplaceAll(path, `\`, "/")
			if reports[path] && strings.Contains(receipt.Output, "### คำพูดในคลิป") && strings.Contains(receipt.Output, "### ข้อมูลค้นเพิ่ม") && strings.Contains(receipt.Output, "### ข้อจำกัด") {
				readEvent, readCall = event.ID, callID
			}
		}
	}
	return readEvent, readCall
}
