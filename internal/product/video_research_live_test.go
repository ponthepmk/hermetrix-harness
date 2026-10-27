package product

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hermetrix-harness/internal/agent"
	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/providers"
	hruntime "hermetrix-harness/internal/runtime"
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
	toolruntime "hermetrix-harness/internal/tools"
)

// Explicit opt-in finite source integration, using actual installed local
// models/media and the real owned command runner. No live daemon deployment,
// qualification exception, automatic write grant or replaced model response.
func TestLiveHermetrixBonsaiVideoResearchOwnedWorkflow(t *testing.T) {
	root := os.Getenv("HERMETRIX_VIDEO_SOURCE_ROOT")
	if root == "" {
		t.Skip("set the actual video research project root to run local inference")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()
	response, err := http.Get("http://127.0.0.1:8088/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	var loaded struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	err = json.NewDecoder(response.Body).Decode(&loaded)
	response.Body.Close()
	if err != nil || len(loaded.Data) != 1 {
		t.Fatal("single actual Bonsai model required", err)
	}
	data, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	skillService := skills.NewService(data)
	runner := NewService(data, skillService)
	defer runner.Close()
	project, err := runner.SaveProject(ctx, ProjectInput{Name: "Actual video research source verification", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	providerService := providers.NewService(data, providers.NewOpenAIAdapter(http.DefaultClient))
	provider, err := providerService.Save(ctx, providers.SaveInput{Name: "Actual Bonsai source verification", AdapterKind: providers.AdapterOpenAICompatible, BaseURL: "http://127.0.0.1:8088/v1", Model: loaded.Data[0].ID, ContextWindow: 65536, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	// Preserve a stricter heavy-reasoning history in this temporary diagnostic;
	// the live provider's observations are neither cleared nor overwritten.
	for i := 0; i < 10; i++ {
		if err := providerService.ObserveReasoning(ctx, provider.ID, 95, 100); err != nil {
			t.Fatal(err)
		}
	}
	estimator := ctxcompiler.NewAdaptiveEstimator()
	compiler := ctxcompiler.NewCompiler(estimator, ctxcompiler.NewBlobSpiller(data.Blobs), ctxcompiler.StructuredCompactor{})
	registry, err := toolruntime.NewRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	controller := agent.NewService(data, providerService, compiler, estimator, hruntime.NewInferenceGate(), registry, skillService).WithRuntime(runner, nil)
	session, err := controller.CreateSession(ctx, agent.CreateSessionInput{Title: "Actual YouTube owned workflow", ProviderID: provider.ID, ProjectID: project.ID, ContextProfile: "compact-32k", ChatReasoning: &agent.ChatReasoningSetting{Mode: "disabled", TokenCap: 0}})
	if err != nil {
		t.Fatal(err)
	}
	if session.Contract.Qualification.Mode != "compatibility" || session.Contract.WorkspaceWriteApproval != nil {
		t.Fatal("unexpected authority")
	}
	prompt := `ช่วยค้นคว้าคลิป https://www.youtube.com/watch?v=jNQXAC9IVRw แล้วสรุปเป็นภาษาไทย คำถามคือ สรุปคำพูด ภาพ และประวัติคลิปจากหลักฐานจริง ขอถอดจากเสียงจริงแม้มีซับ ตรวจภาพตัวอย่างสองช่วง และอ่าน https://blog.youtube/news-and-events/celebrating-10-years-of-youtube/ เป็นแหล่งประกอบ ใช้คู่มือและเครื่องมือที่มีในโปรเจกต์นี้ ทำจนได้ผลจริง แล้วแสดงรายงานที่ตรวจแล้วพร้อมเวลา ลิงก์อ้างอิง และข้อจำกัดตามเนื้อหาไฟล์รายงานโดยตรง ไม่เพิ่มคำสรุปอิสระ ไม่เดาเหตุการณ์หรือวันถ่าย ไม่แก้โค้ดหรือการตั้งค่า ถ้ายังทำงานให้ติดตามงานเดิม ไม่เริ่มซ้ำ และห้ามบอกว่าเสร็จจากสถานะกำลังทำงาน`
	started := time.Now()
	result, turnErr := controller.RunTurn(ctx, session.ID, agent.TurnInput{Content: prompt}, nil)
	detail, detailErr := controller.GetSessionDetail(context.Background(), session.ID)
	jobs, jobsErr := runner.ListJobs(context.Background(), 20)
	if evidence := os.Getenv("HERMETRIX_VIDEO_QA_EVIDENCE"); evidence != "" {
		body, _ := json.MarshalIndent(map[string]any{"result": result, "session": detail, "jobs": jobs, "turn_error": errorText(turnErr), "duration_ms": time.Since(started).Milliseconds()}, "", "  ")
		if err := os.WriteFile(filepath.Join(evidence, "hermetrix-bonsai-source-workflow.json"), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if turnErr != nil || detailErr != nil || jobsErr != nil {
		t.Fatal(turnErr, detailErr, jobsErr)
	}
	completed, reportRead := false, false
	for _, job := range jobs {
		output, _ := job.Result["output"].(string)
		exit, _ := job.Result["exit_code"].(float64)
		if job.State == "completed" && exit == 0 && strings.Contains(output, `"verified_result": true`) {
			completed = true
		}
	}
	for _, event := range detail.Events {
		if event.EventKind != "tool_result" {
			continue
		}
		var receipt toolruntime.Receipt
		if json.Unmarshal([]byte(event.Content), &receipt) == nil && receipt.Name == "workspace.read_file" && receipt.Status == "succeeded" && strings.Contains(receipt.Output, "### คำพูดในคลิป") && strings.Contains(receipt.Output, "### ข้อมูลค้นเพิ่ม") {
			reportRead = true
		}
	}
	if !completed || !reportRead || len(jobs) != 1 || len(detail.Approvals) != 0 || !strings.Contains(result.AssistantEvent.Content, "### คำพูดในคลิป") || strings.ContainsAny(result.AssistantEvent.Content, "引用象") {
		t.Fatalf("actual workflow incomplete: completed=%v reportRead=%v approvals=%d answer=%s", completed, reportRead, len(detail.Approvals), result.AssistantEvent.Content)
	}
	t.Logf("Actual Bonsai commanded real CLI, waited for terminal result, read actual report and answered; %dms", time.Since(started).Milliseconds())
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
