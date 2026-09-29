package taskengine

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRejectedProposalFeedbackKeepsRecentCorrectionsForSameStep(t *testing.T) {
	service, dataStore := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	run, err := service.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, Owner: "runner", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	task, err = service.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := service.BeginStepAttempt(ctx, BeginAttemptInput{RunID: run.ID, LeaseToken: run.LeaseToken,
		StepKey: task.Plan.Steps[0].Key, ExpectedTaskRevision: task.Revision, ExpectedStepRevision: task.Plan.Steps[0].Revision, InputHash: "sha256:packet"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = dataStore.DB.ExecContext(ctx, `INSERT INTO artifacts(id,name,kind,mime_type,blob_ref,byte_size,checksum,metadata_json,created_at)
		VALUES('feedback-artifact','feedback','test','application/json','blob:test',2,'sha256:result','{}',?)`, now); err != nil {
		t.Fatal(err)
	}
	for i, rationale := range []string{"Use the real API", "Keep the create button"} {
		proposalID := "feedback-proposal-" + string(rune('a'+i))
		created := time.Now().UTC().Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano)
		if _, err = dataStore.DB.ExecContext(ctx, `INSERT INTO task_code_proposals
			(id,task_id,step_id,attempt_id,project_id,packet_hash,provider_id,provider_revision,artifact_id,result_hash,state,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,'rejected',?,?)`, proposalID, task.ID, attempt.StepID, attempt.ID,
			"project-test", "sha256:packet", "worker", "1", "feedback-artifact", "sha256:result", created, created); err != nil {
			t.Fatal(err)
		}
		if _, err = dataStore.DB.ExecContext(ctx, `INSERT INTO task_code_reviews
			(id,proposal_id,reviewer,verdict,rationale,findings_json,created_at)
			VALUES(?,?,'owner','rejected',?,'[]',?)`, "feedback-review-"+proposalID, proposalID, rationale, created); err != nil {
			t.Fatal(err)
		}
	}
	got, err := service.LatestRejectedProposalFeedback(ctx, task.ID, attempt.StepID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(got, "Use the real API") < 0 || strings.Index(got, "Keep the create button") <= strings.Index(got, "Use the real API") {
		t.Fatalf("prior feedback was lost or reordered: %q", got)
	}
}
