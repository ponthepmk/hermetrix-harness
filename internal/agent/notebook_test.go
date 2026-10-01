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

	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/store"
	toolruntime "hermetrix-harness/internal/tools"
)

func TestLargeFileEvidenceIsSavedBeforeAnalysisAndSurvivesRestart(t *testing.T) {
	service, provider, cleanup := testAgentService(t, httptest.NewServer(http.NotFoundHandler()))
	defer cleanup()
	ctx := context.Background()
	projectID := createTestProject(t, service, t.TempDir())
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID, ProjectID: projectID, ContextProfile: "compact-16k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.appendEvent(ctx, Event{SessionID: session.ID, TurnID: "initial", EventKind: "message", Role: "user", Content: "ตรวจ architecture และทำต่อจากสิ่งที่อ่านไว้ ห้ามแก้ไฟล์"})
	if err != nil {
		t.Fatal(err)
	}
	var source Event
	for i := 0; i < 60; i++ {
		output := strings.Repeat(fmt.Sprintf("file %d unrelated line\n", i), 1500) + "MID_FILE_FACT_927" + strings.Repeat(" trailing line\n", 1500)
		encoded, _ := json.Marshal(toolruntime.Receipt{Name: "workspace.read_file", Status: "succeeded", Output: output, Metadata: map[string]any{"path": fmt.Sprintf("module-%d.go", i), "sha256": "source-revision"}})
		source, err = service.appendEvent(ctx, Event{SessionID: session.ID, TurnID: "initial", EventKind: "tool_result", Role: "tool", Content: string(encoded), Metadata: map[string]any{"tool_call_id": fmt.Sprintf("read-%d", i), "tool_name": "workspace.read_file"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	paths, _ := filepath.Glob(filepath.Join(service.notebookDir(session.ID), "*.md"))
	if len(paths) != 60 {
		t.Fatalf("evidence not saved before compilation: %d", len(paths))
	}
	dataRoot := service.store.Root
	if err := service.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(ctx, dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	service.store = reopened
	events, err := service.ListEvents(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	notebook, err := service.Notebook(ctx, session.ID)
	if err != nil || !strings.Contains(notebook, "module-59.go") || !strings.Contains(notebook, "ห้ามแก้ไฟล์") {
		t.Fatalf("restart lost progress: %v", err)
	}
	for turn := 0; turn < 12; turn++ {
		goal, err := service.appendEvent(ctx, Event{SessionID: session.ID, TurnID: fmt.Sprint(turn), EventKind: "message", Role: "user", Content: "ทำต่อจากเอกสารเดิม โดยดึงเฉพาะส่วนที่จำเป็น"})
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, goal)
		profile := ctxcompiler.Compact16K()
		compiled, _, err := service.compileTurn(ctx, profile, events, goal.TurnID, session.Contract, ctxcompiler.ScriptEstimator{Scale: 1, NonASCIIRate: .55}, TransportOverhead{})
		if err != nil {
			t.Fatalf("follow-up %d overflowed: %v", turn, err)
		}
		if compiled.Report.UnaccountedTokens != 0 || compiled.Report.PredictedInput > profile.Total-profile.OutputReserve-profile.UncertaintyReserve {
			t.Fatalf("invalid budget: %+v", compiled.Report)
		}
		rendered := fmt.Sprint(renderMessages(compiled.Fragments))
		if strings.Contains(rendered, "MID_FILE_FACT_927") {
			t.Fatal("archived file body was replayed instead of retrieved")
		}
	}
	var receipt toolruntime.Receipt
	_ = json.Unmarshal([]byte(source.Content), &receipt)
	markerOffset := strings.Index(receipt.Output, "MID_FILE_FACT_927")
	page, err := savedEvidencePage(events, "event:"+source.ID+fmt.Sprintf(" offset:%d", markerOffset))
	if err != nil || !strings.Contains(page["content"].(string), "MID_FILE_FACT_927") {
		t.Fatalf("saved middle evidence unreachable: %v", err)
	}
	if len([]rune(page["content"].(string))) > contextSearchMaxContent {
		t.Fatal("retrieval exceeded page budget")
	}
}

func TestSavedEvidencePagingIsScopedAndUnicodeSafe(t *testing.T) {
	events := []Event{{ID: "own", EventKind: "message", Content: strings.Repeat("ก", 3000) + "END"}}
	page, err := savedEvidencePage(events, "event:own offset:2400")
	if err != nil || page["next_offset"] != 3003 || !strings.HasSuffix(page["content"].(string), "END") {
		t.Fatalf("bad final page: %+v %v", page, err)
	}
	for _, query := range []string{"event:other offset:0", "event:own offset:-1", "event:own offset:99999", "event:own offset:x"} {
		if _, err := savedEvidencePage(events, query); err == nil {
			t.Fatalf("invalid source/page accepted: %s", query)
		}
	}
}

func TestEvidenceWriteFailureDoesNotRecordAnUnsavedReceipt(t *testing.T) {
	service, provider, cleanup := testAgentService(t, httptest.NewServer(http.NotFoundHandler()))
	defer cleanup()
	session, err := service.CreateSession(t.Context(), CreateSessionInput{ProviderID: provider.ID, ContextProfile: "compact-32k"})
	if err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	service.store.Root = blocker
	_, err = service.appendEvent(t.Context(), Event{SessionID: session.ID, EventKind: "tool_result", Role: "tool", Content: `{"status":"succeeded","output":"evidence"}`})
	if err == nil {
		t.Fatal("unarchived receipt was committed")
	}
	events, err := service.ListEvents(t.Context(), session.ID)
	if err != nil || len(events) != 0 {
		t.Fatalf("unexpected event commit: %+v %v", events, err)
	}
}

func TestWorkingNotebookRemainsBoundedWithCalibratedThaiContext(t *testing.T) {
	service, _, cleanup := testAgentService(t, httptest.NewServer(http.NotFoundHandler()))
	defer cleanup()
	events := []Event{{ID: "goal", SessionID: "budget", EventKind: "message", Role: "user", Content: strings.Repeat("ข้อกำหนดที่ต้องคงไว้ ", 300)}, {ID: "analysis", SessionID: "budget", EventKind: "message", Role: "assistant", Content: strings.Repeat("วิเคราะห์ระบบ ", 300)}, {ID: "read", SessionID: "budget", EventKind: "tool_result", Content: `{"name":"workspace.read_file","status":"succeeded","output":"data"}`}}
	estimator := ctxcompiler.ScriptEstimator{Scale: 2.5, NonASCIIRate: .55}
	notebook, err := service.prepareNotebook(events, "", estimator)
	if err != nil {
		t.Fatal(err)
	}
	if estimator.Count(notebook.Content) > 512 {
		t.Fatalf("working state overflowed: %d", estimator.Count(notebook.Content))
	}
}

func TestDeletingAChatRemovesItsDerivedEvidenceDocuments(t *testing.T) {
	service, provider, cleanup := testAgentService(t, httptest.NewServer(http.NotFoundHandler()))
	defer cleanup()
	session, err := service.CreateSession(t.Context(), CreateSessionInput{ProviderID: provider.ID, ContextProfile: "compact-32k"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.appendEvent(t.Context(), Event{SessionID: session.ID, EventKind: "tool_result", Role: "tool", Content: `{"name":"workspace.read_file","status":"succeeded","output":"private evidence"}`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Notebook(t.Context(), session.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteSession(t.Context(), session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(service.notebookDir(session.ID)); !os.IsNotExist(err) {
		t.Fatalf("derived transcript survived deletion: %v", err)
	}
}

func TestWorkingEvidenceFitsInlineBudgetWithoutLosingTheFocusedFact(t *testing.T) {
	estimator := ctxcompiler.ScriptEstimator{Scale: 2.5, NonASCIIRate: .55}
	source := strings.Repeat("หลักฐานที่ไม่เกี่ยวข้อง ", 1000) + " IMPORTANT_MIDDLE_927 " + strings.Repeat("ข้อมูลเพิ่มเติม ", 1000)
	data, _ := json.Marshal(toolruntime.Receipt{Name: "workspace.read_file", Status: "succeeded", Output: source})
	event := Event{ID: "source", Content: string(data)}
	projected := projectedEvidence(event, "IMPORTANT_MIDDLE_927", true, estimator, 800)
	if estimator.Count(projected) > 800 || !strings.Contains(projected, "IMPORTANT_MIDDLE_927") {
		t.Fatalf("focused evidence would be spilled again: %d", estimator.Count(projected))
	}
}

func TestWorkingRetrievalPreservesPagingOffsetsWhenShrunk(t *testing.T) {
	estimator := ctxcompiler.ScriptEstimator{Scale: 2.5, NonASCIIRate: .55}
	payload, _ := json.Marshal(map[string]any{"event_id": "source", "content": strings.Repeat("ก", 1200), "offset": 1200, "next_offset": 2400, "total_runes": 5000, "has_more": true})
	data, _ := json.Marshal(toolruntime.Receipt{Name: "context_search", Status: "succeeded", Output: string(payload)})
	projected := projectedEvidence(Event{ID: "search", Content: string(data)}, "fact", true, estimator, 800)
	var result toolruntime.Receipt
	if err := json.Unmarshal([]byte(projected), &result); err != nil {
		t.Fatal(err)
	}
	var page map[string]any
	if err := json.Unmarshal([]byte(result.Output), &page); err != nil {
		t.Fatal(err)
	}
	returned := len([]rune(page["content"].(string)))
	if page["next_offset"].(float64) != float64(1200+returned) || estimator.Count(projected) > 800 {
		t.Fatalf("invalid working page: %+v", page)
	}
}
