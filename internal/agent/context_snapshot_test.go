package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	ctxcompiler "hermetrix-harness/internal/context"
)

func TestSessionDetailReturnsLatestCompiledContextReport(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	ctx := context.Background()
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "compact-32k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := service.GetSessionDetail(ctx, session.ID)
	if err != nil || detail.LatestContextSnapshot != nil {
		t.Fatalf("empty session snapshot = %+v, %v", detail.LatestContextSnapshot, err)
	}
	for index, selected := range []string{"old-goal", "current-goal"} {
		report, err := json.Marshal(ctxcompiler.Report{Profile: "compact-32k", PredictedPrompt: 100 + index,
			SelectedIDs: []string{selected}, DroppedIDs: []string{"older-tool"}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.store.DB.ExecContext(ctx, `INSERT INTO context_snapshots(id,session_id,turn_id,provider_id,model,
			profile_name,compiled_json,report_json,created_at) VALUES(?,?,?,?,?,?,'{}',?,?)`,
			selected, session.ID, "turn", provider.ID, provider.Model, "compact-32k", string(report),
			time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			t.Fatal(err)
		}
	}
	detail, err = service.GetSessionDetail(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.LatestContextSnapshot == nil || detail.LatestContextSnapshot.ID != "current-goal" ||
		detail.LatestContextSnapshot.Report.PredictedPrompt != 101 ||
		len(detail.LatestContextSnapshot.Report.SelectedIDs) != 1 ||
		detail.LatestContextSnapshot.Report.SelectedIDs[0] != "current-goal" {
		t.Fatalf("latest context report = %+v", detail.LatestContextSnapshot)
	}
	if _, err := service.store.DB.ExecContext(ctx, `UPDATE context_snapshots SET report_json='{' WHERE id='current-goal'`); err != nil {
		t.Fatal(err)
	}
	detail, err = service.GetSessionDetail(ctx, session.ID)
	if err != nil || detail.LatestContextSnapshot != nil || detail.ContextSnapshotError == "" {
		t.Fatalf("damaged report blocked conversation: detail=%+v err=%v", detail, err)
	}
}
