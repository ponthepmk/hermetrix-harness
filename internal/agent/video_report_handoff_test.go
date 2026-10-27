package agent

import (
	"encoding/json"
	"strings"
	"testing"

	toolruntime "hermetrix-harness/internal/tools"
)

func TestVideoReportHandoffRequiresMatchingCompletedJobAndActualRead(t *testing.T) {
	path := ".research-jobs/" + strings.Repeat("a", 32) + "/report.md"
	receiptEvent := func(id, call, name, output string, metadata map[string]any) Event {
		content, _ := json.Marshal(toolruntime.Receipt{ToolCallID: call, Name: name, Status: "succeeded", Output: output, Metadata: metadata})
		return Event{ID: id, TurnID: "turn", EventKind: "tool_result", Role: "tool", Content: string(content), Metadata: map[string]any{"tool_call_id": call}}
	}
	callEvent := func(id, name, args string) Event {
		return Event{TurnID: "turn", EventKind: "tool_call", Content: args, Metadata: map[string]any{"tool_call_id": id, "tool_name": name}}
	}
	status := `{"verified_result":true,"report_file":"` + path + `"}`
	events := []Event{
		{TurnID: "turn", EventKind: "message", Role: "user", Content: "สรุปคลิป https://youtu.be/jNQXAC9IVRw"},
		callEvent("start", "workspace.run", `{"action":"start","executable":"node","arguments":["cli.mjs","--url","https://youtu.be/jNQXAC9IVRw"]}`),
		receiptEvent("start-event", "start", "workspace.run", "started", map[string]any{"job_id": "job"}),
		callEvent("status", "workspace.run", `{"action":"status","job_id":"job"}`),
		receiptEvent("done", "status", "workspace.run", status, map[string]any{"job_id": "job", "state": "completed", "exit_code": 0}),
		callEvent("read", "workspace.read_file", `{"path":"`+path+`"}`),
		receiptEvent("read-event", "read", "workspace.read_file", "### คำพูดในคลิป\n### ข้อมูลค้นเพิ่ม\n### ข้อจำกัด", nil),
	}
	if event, call := completedVideoReportRead(events, "turn"); event != "read-event" || call != "read" {
		t.Fatalf("handoff not bound: %s %s", event, call)
	}
	for _, cut := range []int{3, 5, 6} {
		if event, _ := completedVideoReportRead(events[:cut], "turn"); event != "" {
			t.Fatal("premature handoff")
		}
	}
	if event, _ := completedVideoReportRead(events, "different-turn"); event != "" {
		t.Fatal("cross-turn handoff")
	}
	for _, change := range []func([]Event){
		func(e []Event) {
			e[0].Content = "แก้โค้ดและสรุปคลิป https://youtu.be/jNQXAC9IVRw"
		},
		func(e []Event) {
			e[4] = receiptEvent("done", "status", "workspace.run", status, map[string]any{"job_id": "job", "state": "failed", "exit_code": 1})
		},
		func(e []Event) {
			e[4] = receiptEvent("done", "status", "workspace.run", status, map[string]any{"job_id": "other-job", "state": "completed", "exit_code": 0})
		},
		func(e []Event) {
			e[4] = receiptEvent("done", "status", "workspace.run", strings.ReplaceAll(status, "true", "false"), map[string]any{"job_id": "job", "state": "completed", "exit_code": 0})
		},
		func(e []Event) {
			e[6] = receiptEvent("read-event", "read", "workspace.read_file", "only a truncated preview", nil)
		},
	} {
		copyEvents := append([]Event(nil), events...)
		change(copyEvents)
		if event, _ := completedVideoReportRead(copyEvents, "turn"); event != "" {
			t.Fatal("invalid evidence produced a completion handoff")
		}
	}
}
