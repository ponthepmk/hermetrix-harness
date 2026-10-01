package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/providers"
)

// A provider can ignore tool_choice or return stop with no content. Keep the
// canonical receipts and give the user a truthful, nonempty progress report.
// This is a partial outcome, never evidence that the requested review passed.
func (s *Service) finishEvidenceSummary(ctx context.Context, session Session, provider providers.Profile, turnID string, binding StepBinding, report ctxcompiler.Report, usage providers.Usage, reason string) (TurnResult, error) {
	events, err := s.ListEvents(ctx, session.ID)
	if err != nil {
		return TurnResult{}, err
	}
	var lines []string
	for _, event := range events {
		if event.TurnID != turnID || event.EventKind != "tool_result" {
			continue
		}
		var receipt struct {
			Status string          `json:"status"`
			Output json.RawMessage `json:"output"`
		}
		_ = json.Unmarshal([]byte(event.Content), &receipt)
		name, _ := event.Metadata["tool_name"].(string)
		label := map[string]string{"workspace.run": "รันคำสั่ง", "workspace.read_file": "อ่านไฟล์และบันทึกข้อมูล", "workspace.list": "สำรวจรายการไฟล์", "context_search": "ค้นข้อมูลจากเอกสารบริบท"}[name]
		if label == "" {
			label = "เรียกเครื่องมือ"
		}
		status := "ยังไม่สำเร็จ"
		if receipt.Status == "succeeded" {
			status = "เสร็จแล้ว"
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", label, status))
	}
	count := len(lines)
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	text := fmt.Sprintf("งานรอบนี้ยังไม่เสร็จครบ ข้อมูลที่ตรวจไว้ถูกบันทึกในเอกสารบริบทแล้ว\n\nมีผลเครื่องมือที่บันทึกไว้ %d รายการ", count)
	if len(lines) > 0 {
		text += " (แสดงล่าสุด)\n\n" + strings.Join(lines, "\n")
	}
	text += "\n\nโมเดลยังไม่ส่งคำตอบสรุปที่ใช้งานได้ หรือรอบนี้ถึงขีดจำกัดการทำงาน จึงหยุดการเรียกเครื่องมือเพิ่ม ยังยืนยันไม่ได้ว่ารีวิวโปรเจกต์ครบหรือไม่มีข้อผิดพลาด กด **ทำต่อจากเอกสาร** เพื่อทำส่วนที่เหลือจากหลักฐานเดิม"
	event, err := s.completeTurn(ctx, session, provider, Event{SessionID: session.ID, TurnID: turnID, EventKind: "message", Role: "assistant", Content: text, ProviderID: provider.ID, Model: provider.Model, CreatedAt: time.Now().UTC(), Metadata: map[string]any{"usage": usage, "finish_reason": "partial", "continuation_needed": true, "continuation_reason": reason, "summary_fallback": true, "step_binding_id": binding.ID, "context_snapshot_id": binding.ContextSnapshotID}})
	if err != nil {
		return TurnResult{}, err
	}
	return TurnResult{TurnID: turnID, AssistantEvent: event, Binding: binding, ContextReport: report, Usage: usage, FinishReason: "partial"}, nil
}
