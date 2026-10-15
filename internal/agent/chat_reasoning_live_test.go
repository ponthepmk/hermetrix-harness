package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hermetrix-harness/internal/providers"
	toolruntime "hermetrix-harness/internal/tools"
)

// Explicit opt-in source verification against the user's already-running local
// model. Temporary DB/workspace only; no UI deployment or live-instance restart.
func TestLiveBonsaiCappedAdmissionAndActualRead(t *testing.T) {
	base := strings.TrimRight(os.Getenv("HERMETRIX_REAL_BONSAI_URL"), "/")
	if base == "" {
		t.Skip("set HERMETRIX_REAL_BONSAI_URL to opt into the actual local-model check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/models", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var models struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	err = json.NewDecoder(response.Body).Decode(&models)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || len(models.Data) != 1 {
		t.Fatalf("expected the single already-loaded local model: %+v err=%v", models, err)
	}
	fixture := httptest.NewServer(http.NotFoundHandler())
	root := t.TempDir()
	service, _, cleanup := testAgentServiceAtRoot(t, fixture, root)
	defer cleanup()
	provider, err := service.providers.Save(ctx, providers.SaveInput{Name: "Actual local capped admission check", AdapterKind: providers.AdapterOpenAICompatible, BaseURL: base + "/v1", Model: models.Data[0].ID, ContextWindow: 65536, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if err := service.providers.ObserveReasoning(ctx, provider.ID, 95, 100); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "READ_PROOF.txt"), []byte("LIVE_CAPPED_ADMISSION_OK\n"), 0600); err != nil {
		t.Fatal(err)
	}
	project := createTestProject(t, service, root)
	session, err := service.CreateSession(ctx, CreateSessionInput{Title: "Live capped admission read-only check", ProviderID: provider.ID, ProjectID: project, ContextProfile: "compact-32k", ChatReasoning: &ChatReasoningSetting{Mode: "bounded", TokenCap: 512}})
	if err != nil {
		t.Fatal(err)
	}
	if session.Contract.AnswerBudget != 3584 || session.Contract.Qualification.Mode != "compatibility" || session.Contract.WorkspaceWriteApproval != nil {
		t.Fatal("unexpected reserve or authority")
	}
	started := time.Now()
	result, err := service.RunTurn(ctx, session.ID, TurnInput{Content: "งานตรวจแหล่งข้อมูลแบบอ่านอย่างเดียว ใช้ workspace.read_file อ่าน READ_PROOF.txt ของโปรเจกต์นี้จริง แล้วตอบเฉพาะข้อความที่พบในไฟล์ ห้ามคาดเดา ห้ามเขียนไฟล์ รันคำสั่ง หรือใช้เครื่องมืออื่น จบหลังอ่านสำเร็จครั้งเดียว"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := service.GetSessionDetail(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	read := false
	for _, event := range detail.Events {
		if event.EventKind != "tool_result" {
			continue
		}
		var receipt toolruntime.Receipt
		if json.Unmarshal([]byte(event.Content), &receipt) == nil && receipt.Name == "workspace.read_file" && receipt.Status == "succeeded" && strings.Contains(receipt.Output, "LIVE_CAPPED_ADMISSION_OK") {
			read = true
		}
	}
	if !read || !strings.Contains(result.AssistantEvent.Content, "LIVE_CAPPED_ADMISSION_OK") || len(detail.Approvals) != 0 || detail.Session.State != "active" {
		t.Fatalf("actual read/answer did not finish: read=%v result=%+v approvals=%d", read, result, len(detail.Approvals))
	}
	if evidence := os.Getenv("HERMETRIX_REAL_MODEL_EVIDENCE_DIR"); evidence != "" {
		body, _ := json.MarshalIndent(map[string]any{"model": provider.Model, "answer_budget": session.Contract.AnswerBudget, "duration_ms": time.Since(started).Milliseconds(), "result": result, "session": detail}, "", "  ")
		if err := os.WriteFile(filepath.Join(evidence, "live-capped-admission.json"), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Log(fmt.Sprintf("Actual Bonsai accepted capped compact32k despite95%% historical reasoning, read the real fixture and answered; %dms", time.Since(started).Milliseconds()))
}
