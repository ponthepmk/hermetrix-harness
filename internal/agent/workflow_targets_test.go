package agent

import (
	"context"
	"strings"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestWorkflowConsentUsesExactBrowserAndDesktopReceipts(t *testing.T) {
	for _, kind := range []string{"browser", "desktop"} {
		t.Run(kind, func(t *testing.T) {
			args := `{"action":"key","tab_id":"game","ref":1,"text":"w","duration_ms":500,"observation_hash":"` + strings.Repeat("a", 64) + `"}`
			if kind == "desktop" {
				args = `{"action":"click","handle":1,"process_id":2,"point":{"x":10,"y":20},"observation_hash":"` + strings.Repeat("a", 64) + `"}`
			}
			service, provider, cleanup := testAgentServiceAtRoot(t, writeConsentProvider(t, []providers.ToolCall{{ID: "action", Name: kind, Arguments: args}}), t.TempDir())
			defer cleanup()
			project := createTestProject(t, service, t.TempDir())
			browser := &fakeDriver{page: BrowserPage{TabID: "game", URL: "http://127.0.0.1:18820/", ObservationHash: strings.Repeat("a", 64), ScreenshotArtifactID: "current-screen", Elements: []BrowserPageElement{{Ref: 1, Tag: "canvas", Text: "Game"}}}}
			native := &fixtureDesktop{}
			service.WithRuntime(nil, browser).WithDesktop(native)
			session, err := service.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID, ProjectID: project, ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride(),
				WorkspaceWriteApproval: &WorkspaceWriteApprovalInput{Reason: "Owner explicitly selected game targets", BrowserOrigins: []string{"http://127.0.0.1:18820"}, DesktopWindows: []DesktopWindowApproval{{Handle: 1, ProcessID: 2}}}})
			if err != nil {
				t.Fatal(err)
			}
			pauses := 0
			result, err := service.RunTurn(context.Background(), session.ID, TurnInput{Content: "act within selected game target"}, func(event StreamEvent) error {
				if event.Type == "approval_required" {
					pauses++
				}
				return nil
			})
			if err != nil || pauses != 0 || result.Approval != nil {
				t.Fatalf("workflow paused %+v err=%v", result, err)
			}
			if kind == "browser" && (len(browser.prepared) != 1 || browser.prepared[0].DurationMS != 500) {
				t.Fatal("missing exact held-key preparation")
			}
			if kind == "desktop" && (native.prepared != 1 || native.confirmed != 1) {
				t.Fatal("native exact receipt was not used once")
			}
			detail, err := service.GetSessionDetail(context.Background(), session.ID)
			if err != nil || len(detail.Approvals) != 1 || detail.Approvals[0].State != "executed" || detail.Approvals[0].Metadata["authorization"] != "project-workflow-v1" {
				t.Fatalf("durable grant missing %+v %v", detail, err)
			}
			if _, err := service.DecideApproval(context.Background(), detail.Approvals[0].ID, ApprovalDecisionInput{Actor: "user", Decision: "approve"}, nil); err == nil {
				t.Fatal("workflow action replayed")
			}
		})
	}
}

func TestWorkflowConsentDoesNotAuthorizeOtherOriginsOrWindows(t *testing.T) {
	for _, kind := range []string{"browser", "desktop"} {
		t.Run(kind, func(t *testing.T) {
			args := `{"action":"open","url":"https://unselected.example/"}`
			if kind == "desktop" {
				args = `{"action":"click","handle":9,"process_id":99,"point":{"x":1,"y":1},"observation_hash":"` + strings.Repeat("a", 64) + `"}`
			}
			service, provider, cleanup := testAgentServiceAtRoot(t, writeConsentProvider(t, []providers.ToolCall{{ID: "outside", Name: kind, Arguments: args}}), t.TempDir())
			defer cleanup()
			project := createTestProject(t, service, t.TempDir())
			native := &fixtureDesktop{}
			browser := &fakeDriver{}
			service.WithRuntime(nil, browser).WithDesktop(native)
			session, err := service.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID, ProjectID: project, ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride(), WorkspaceWriteApproval: &WorkspaceWriteApprovalInput{Reason: "Only selected target", BrowserOrigins: []string{"http://127.0.0.1:18820"}, DesktopWindows: []DesktopWindowApproval{{Handle: 1, ProcessID: 2}}}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.RunTurn(context.Background(), session.ID, TurnInput{Content: "request another target"}, nil)
			if err != nil || result.FinishReason != "approval_required" || len(browser.opened) != 0 || native.confirmed != 0 {
				t.Fatalf("unselected target executed %+v %v", result, err)
			}
		})
	}
}
