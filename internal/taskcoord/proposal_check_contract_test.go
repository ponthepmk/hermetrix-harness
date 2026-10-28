package taskcoord

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

type captureAdapter struct {
	inner *goalAdapter
	last  worker.Task
	raw   string
}

func (a *captureAdapter) StreamChat(ctx context.Context, profile providers.Profile, _ string,
	r providers.ChatRequest, send func(providers.Delta) error) (providers.Completion, error) {
	if len(r.Tools) > 0 && r.Tools[0].Function.Name == "submit_changes" {
		a.raw = r.Messages[1].Content
		if err := json.Unmarshal([]byte(r.Messages[1].Content), &a.last); err != nil {
			return providers.Completion{}, fmt.Errorf("decode submit_changes: %w", err)
		}
	}
	return a.inner.StreamChat(ctx, profile, "", r, send)
}

func testProposalCheckContractProjection(t *testing.T, criteriaCount int) {
	ctx := context.Background()
	base := &goalAdapter{}
	c, ts, task, input, root, db := newGoalFixtureWithStore(t, base, taskengine.GoalContract{})
	originalBytes, originalReadErr := os.ReadFile(filepath.Join(root, "sum.js"))
	if originalReadErr != nil {
		t.Fatal(originalReadErr)
	}
	capture := &captureAdapter{inner: base}
	c.providers = providers.NewService(db, capture)

	ids := make([]string, criteriaCount)
	for i := range ids {
		ids[i] = fmt.Sprintf("AC-%d", i+1)
	}
	criteria := make([]taskengine.Criterion, criteriaCount)
	for i := range criteria {
		criteria[i] = taskengine.Criterion{ID: ids[i], Description: fmt.Sprintf("acceptance criterion %d", i+1)}
	}
	checks32 := make([]string, 32)
	for i := range checks32 {
		checks32[i] = fmt.Sprintf("node test.js --case=%d", i+1)
	}

	specs := []taskengine.StepSpec{{Key: "fix", Title: "Correct sum.js arithmetic",
		Instructions: "Replace the broken subtraction with addition while preserving module.exports and newline. Preserve test.js unchanged. Verify each generated case passes node test.js --case=N.", RequirementIDs: ids, Checks: checks32,
		EffectScope: []string{"provider.select_files", "provider.propose", "workspace.apply", "workspace.run", "provider.review"}}}

	task, err := ts.ReviseRequirements(ctx, taskengine.ReviseRequirementsInput{TaskID: task.ID,
		ExpectedTaskRevision: task.Revision, Constraints: task.Requirement.Constraints, Criteria: criteria, Actor: "owner"})
	if err != nil {
		t.Fatalf("ReviseRequirements: %v", err)
	}

	task, err = ts.CreatePlan(ctx, taskengine.CreatePlanInput{TaskID: task.ID,
		ExpectedTaskRevision: task.Revision, RequirementRevision: task.ActiveRequirementRevision, Reason: "bounded projection fixture", Actor: "planner", Steps: specs})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	packet, err := ts.BuildNextStepPacket(ctx, task.ID)
	if err != nil {
		t.Fatalf("BuildNextStepPacket: %v", err)
	}

	run, err := ts.BeginRun(ctx, taskengine.BeginRunInput{TaskID: task.ID,
		ExpectedTaskRevision: task.Revision, Owner: "coordinator", LeaseDuration: 5 * time.Minute})
	if err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	task, err = ts.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	attempt, err := ts.BeginStepAttempt(ctx, taskengine.BeginAttemptInput{RunID: run.ID,
		LeaseToken: run.LeaseToken, StepKey: "fix",
		ExpectedTaskRevision: task.Revision,
		ExpectedStepRevision: task.Plan.Steps[0].Revision,
		InputHash:            packet.CanonicalPacketHash, Packet: &packet})
	if err != nil {
		t.Fatalf("BeginStepAttempt: %v", err)
	}

	authority := taskengine.RunAuthority{RunID: run.ID, LeaseToken: run.LeaseToken}
	output, err := c.Propose(ctx, ProposalInput{
		AttemptID: attempt.ID, ProviderID: input.ProviderID,
		Authority: authority, Packet: packet, Files: []string{"sum.js", "test.js"},
	})
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}

	if base.proposals != 1 {
		t.Fatalf("proposals=%d want 1", base.proposals)
	}
	if len(capture.last.AcceptanceCriteria) != criteriaCount {
		t.Fatalf("acceptance criteria count=%d want %d", len(capture.last.AcceptanceCriteria), criteriaCount)
	}
	expectedCriteria := stepCriteria(packet)
	if !reflect.DeepEqual(capture.last.AcceptanceCriteria, expectedCriteria) {
		t.Fatalf("AcceptanceCriteria mismatch:\n got %v\n want %v", capture.last.AcceptanceCriteria, expectedCriteria)
	}
	if len(capture.last.RecommendedChecks) != 32 {
		t.Fatalf("recommended checks count=%d want 32 (truncation?)", len(capture.last.RecommendedChecks))
	}
	if !reflect.DeepEqual(capture.last.RecommendedChecks, packet.Step.Checks) {
		t.Fatalf("RecommendedChecks mismatch:\n got %v\n want %v", capture.last.RecommendedChecks, packet.Step.Checks)
	}
	if output.Result.InputSHA256 != worker.Hash(capture.raw) {
		t.Fatalf("InputSHA256 mismatch: got %s want %s", output.Result.InputSHA256, worker.Hash(capture.raw))
	}
	sumBytes, err := os.ReadFile(filepath.Join(root, "sum.js"))
	if err != nil {
		t.Fatalf("read sum.js: %v", err)
	}
	if string(sumBytes) != string(originalBytes) {
		t.Fatalf("sum.js source changed: got %q", string(sumBytes))
	}
}

func TestProposalCheckContractProjection(t *testing.T) {
	for _, n := range []int{2, 32} {
		t.Run(fmt.Sprintf("criteria_%d", n), func(t *testing.T) {
			testProposalCheckContractProjection(t, n)
		})
	}
}
