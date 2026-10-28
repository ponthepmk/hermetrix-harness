package taskcoord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/inference"
	"hermetrix-harness/internal/learning"
	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

func TestVerifiedTaskCompletionQueuesBoundedLearningDigest(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	skillService := skills.NewService(dataStore)
	learner := learning.NewService(dataStore, skillService, nil, learning.StructuredReviewer{})
	coordinator := (&Service{}).WithLearning(learner)
	task := taskengine.Task{ID: "task-1", Revision: 7, Objective: "fix verified sum",
		Requirement: taskengine.RequirementRevision{Constraints: []string{"local only"}},
		Plan: taskengine.PlanRevision{Steps: []taskengine.Step{{StepSpec: taskengine.StepSpec{
			Title: "Fix addition", Instructions: "change subtract to add"}}}}}
	coordinator.enqueueVerifiedTaskLearning(ctx, task,
		taskengine.CodeProposal{ArtifactID: "proposal-1", VerificationArtifactID: "checks-1"},
		product.Artifact{ID: "review-1"}, verificationBundle{Evidence: []taskengine.Validation{{EvidenceRefs: []string{"job:test-1"}}}})
	jobs, err := learner.List(ctx, learning.StateQueued)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
	if jobs[0].Digest.Outcome != "completed" || len(jobs[0].Digest.VerifiedBy) != 2 || jobs[0].SessionID != "task:task-1" {
		t.Fatalf("digest=%+v", jobs[0])
	}
}

func TestProposalFailureEvidenceUsesBoundedSameProjectCommandLogs(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	products := product.NewService(dataStore, skills.NewService(dataStore))
	defer products.Close()
	project, err := products.EnsureWorkspaceProject(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := products.EnsureWorkspaceProject(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log, err := products.CreateArtifact(ctx, product.ArtifactInput{ProjectID: project.ID, Name: "failed.txt", Kind: "terminal_log", MIMEType: "text/plain", Content: strings.Repeat("ก", 2100) + "actual failure"})
	if err != nil {
		t.Fatal(err)
	}
	foreignLog, err := products.CreateArtifact(ctx, product.ArtifactInput{ProjectID: foreign.ID, Name: "foreign.txt", Kind: "terminal_log", MIMEType: "text/plain", Content: "foreign project secret"})
	if err != nil {
		t.Fatal(err)
	}
	packet := taskengine.StepPacket{ProjectID: project.ID, Step: taskengine.Step{StepSpec: taskengine.StepSpec{Checks: []string{"node test.js"}}}, ValidationEvidence: []taskengine.Validation{
		{ID: "old", CheckID: "node test.js", Status: taskengine.ValidationFail, EvidenceRefs: []string{"artifact:" + foreignLog.ID}},
		{ID: "latest", CheckID: "node test.js", SubjectRevision: "step:2", Status: taskengine.ValidationFail, Actual: "state=failed exit_code=1", EvidenceRefs: []string{"job:test", "artifact:" + foreignLog.ID, "artifact:" + log.ID}},
		{ID: "unrelated", CheckID: "another check", Status: taskengine.ValidationFail, EvidenceRefs: []string{"artifact:" + foreignLog.ID}},
	}}
	evidence, err := (&Service{products: products}).proposalFailureEvidence(ctx, packet)
	if err != nil || len(evidence) != 1 || evidence[0].ValidationID != "latest" || evidence[0].ArtifactID != log.ID || evidence[0].ArtifactSHA256 != log.Checksum || !evidence[0].Truncated || len([]rune(evidence[0].Output)) != 2000 || !strings.HasSuffix(evidence[0].Output, "actual failure") || strings.Contains(evidence[0].Output, "foreign project") {
		t.Fatalf("bounded failure context=%+v err=%v", evidence, err)
	}
}

func TestRollbackReceiptProvesRestoredBytesAndRefusesLaterEdits(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	products := product.NewService(dataStore, skills.NewService(dataStore))
	defer products.Close()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("Status: old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project, err := products.EnsureWorkspaceProject(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	original, err := products.ReadProjectFile(ctx, project.ID, "README.md")
	if err != nil {
		t.Fatal(err)
	}
	proposalID := "proposal-rollback-proof"
	proposalBody, _ := json.Marshal(worker.Result{ProposalID: proposalID,
		Status:  "proposed_unverified",
		Changes: []worker.Change{{Path: "README.md", BeforeSHA256: original.SHA256, Content: "Status: new\n"}}})
	proposalArtifact, err := products.CreateArtifact(ctx, product.ArtifactInput{ProjectID: project.ID,
		Name: "proposal.json", Kind: "code_proposal", MIMEType: "application/json", Content: string(proposalBody)})
	if err != nil {
		t.Fatal(err)
	}
	rollbackBody, _ := json.Marshal(map[string]product.FileDocument{"README.md": original})
	preimage, err := products.CreateArtifact(ctx, product.ArtifactInput{ProjectID: project.ID,
		Name: "preimage.json", Kind: "code_proposal_rollback", MIMEType: "application/json",
		Content: string(rollbackBody), Metadata: map[string]any{"proposal_id": proposalID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := products.WriteProjectFile(ctx, project.ID, product.WriteFileInput{Path: "README.md",
		Content: "Status: new\n", ExpectedSHA256: original.SHA256, Actor: "test"}); err != nil {
		t.Fatal(err)
	}
	proposal := taskengine.CodeProposal{ID: proposalID, ProjectID: project.ID,
		ArtifactID: proposalArtifact.ID, RollbackArtifactID: preimage.ID, ResultHash: proposalArtifact.Checksum}
	coordinator := &Service{products: products}
	outcome := coordinator.rollbackProposal(ctx, proposal)
	if !outcome.Restored || outcome.Artifact.Kind != "code_rollback_receipt" {
		t.Fatalf("rollback outcome=%+v", outcome)
	}
	_, receiptBody, err := products.GetArtifact(ctx, outcome.Artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	var receipt rollbackReceipt
	if err := json.Unmarshal(receiptBody, &receipt); err != nil || !receipt.Restored || len(receipt.Files) != 1 ||
		receipt.Files[0].Status != "restored" || receipt.Files[0].AfterSHA256 != original.SHA256 ||
		strings.Contains(string(receiptBody), "Status: old") {
		t.Fatalf("rollback receipt=%+v body=%s err=%v", receipt, receiptBody, err)
	}
	if again := coordinator.rollbackProposal(ctx, proposal); !again.Restored {
		t.Fatalf("idempotent rollback=%+v", again)
	}
	current, err := products.ReadProjectFile(ctx, project.ID, "README.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := products.WriteProjectFile(ctx, project.ID, product.WriteFileInput{Path: "README.md",
		Content: "Manual edit\n", ExpectedSHA256: current.SHA256, Actor: "test"}); err != nil {
		t.Fatal(err)
	}
	conflict := coordinator.rollbackProposal(ctx, proposal)
	if conflict.Restored || conflict.Artifact.ID == "" {
		t.Fatalf("conflicting edit rollback=%+v", conflict)
	}
	_, conflictBody, err := products.GetArtifact(ctx, conflict.Artifact.ID)
	if err != nil || !strings.Contains(string(conflictBody), "changed_since_apply") {
		t.Fatalf("conflict receipt=%s err=%v", conflictBody, err)
	}
	current, err = products.ReadProjectFile(ctx, project.ID, "README.md")
	if err != nil || current.Content != "Manual edit\n" {
		t.Fatalf("manual edit was overwritten: %+v err=%v", current, err)
	}
}

func TestAutoPlanUsesImmutablePresetWithinNative61440Context(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	products := product.NewService(dataStore, skills.NewService(dataStore))
	defer products.Close()
	projectRoot := t.TempDir()
	if err = os.WriteFile(filepath.Join(projectRoot, "go.mod"), []byte("module plannerfixture\n\ngo 1.25\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(projectRoot, "sum.go"), []byte("package plannerfixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project, err := products.EnsureWorkspaceProject(ctx, projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &proposalAdapter{}
	providerService := providers.NewService(dataStore, adapter)
	profile, err := providerService.Save(ctx, providers.SaveInput{Name: "local 61k", BaseURL: "http://127.0.0.1:8088/v1", Model: "bonsai", ContextWindow: 61440, MaxOutputTokens: 8192})
	if err != nil {
		t.Fatal(err)
	}
	tasks := taskengine.NewService(dataStore)
	task, err := tasks.Create(ctx, taskengine.CreateTaskInput{ProjectID: project.ID, Title: "Fix sum", Objective: "make addition correct", OriginalRequest: "fix add", Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "addition returns sum"}}, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	brain := &fakeBrain{err: errors.New("Pi unreachable")}
	planned, err := New(tasks, products, providerService).WithProjectBrain(project.ID, "team-alpha", brain,
		ctxcompiler.NewCompiler(ctxcompiler.NewAdaptiveEstimator(), nil, nil)).AutoPlan(ctx,
		AutoPlanInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, ProviderID: profile.ID, Actor: "owner"})
	if err != nil || planned.Task.State != taskengine.StateReady || adapter.plannerCalls != 1 {
		t.Fatalf("61k plan failed: task=%+v calls=%d err=%v", planned.Task, adapter.plannerCalls, err)
	}
	if brain.calls != 1 {
		t.Fatalf("configured Project Brain was not attempted before local planning: %d", brain.calls)
	}
	if len(adapter.plannerRequest.Messages) != 2 {
		t.Fatalf("planner request messages=%d", len(adapter.plannerRequest.Messages))
	}
	var plannerInput worker.PlanTask
	if err = json.Unmarshal([]byte(adapter.plannerRequest.Messages[1].Content), &plannerInput); err != nil {
		t.Fatal(err)
	}
	if len(plannerInput.RepositoryFiles) != 2 || plannerInput.RepositoryFiles[0].Path != "sum.go" || plannerInput.RepositoryFiles[1].Path != "go.mod" ||
		len(plannerInput.AllowedExecutables) == 0 {
		t.Fatalf("planner repository context=%+v executables=%v", plannerInput.RepositoryFiles, plannerInput.AllowedExecutables)
	}
	presetID, _ := planned.Artifact.Metadata["preset_id"].(string)
	preset, err := inference.LoadPreset(ctx, dataStore.DB, presetID, 1)
	if err != nil || preset.ContextMax != 55808 || preset.GenerationCap != 5120 || presetID == "planner" {
		t.Fatalf("plan artifact did not identify bounded contract: preset=%+v err=%v", preset, err)
	}
	var ledgerID string
	if err = dataStore.DB.QueryRow(`SELECT preset_id FROM inference_usage_ledger WHERE owner_id=? AND usage_source='planner'`, task.ID).Scan(&ledgerID); err != nil || ledgerID != presetID {
		t.Fatalf("artifact/ledger preset mismatch: artifact=%s ledger=%s err=%v", presetID, ledgerID, err)
	}
	uiTask, err := tasks.Create(ctx, taskengine.CreateTaskInput{ProjectID: project.ID, Title: "Review UI", Objective: "Improve start screen",
		OriginalRequest: "Show the design before work", Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "A user can review the design"}},
		Actor: "owner", Contract: taskengine.GoalContract{RequirePlanReview: true, ExperiencePreview: "A summary and edit action appear"}})
	if err != nil {
		t.Fatal(err)
	}
	coordinator := New(tasks, products, providerService)
	first, err := coordinator.AutoPlan(ctx, AutoPlanInput{TaskID: uiTask.ID, ExpectedTaskRevision: uiTask.Revision,
		ProviderID: profile.ID, Actor: "planner"})
	if err != nil || first.Task.Plan.ReviewStatus != "pending" {
		t.Fatalf("first UI plan = %+v err=%v", first.Task.Plan, err)
	}
	rejected, err := tasks.ReviewPlan(ctx, uiTask.ID, first.Task.Revision, first.Task.Plan.SubjectHash,
		"owner", "rejected", "Show the mobile recovery state before implementation")
	if err != nil || rejected.Plan.ReviewReason != "Show the mobile recovery state before implementation" {
		t.Fatalf("rejection feedback was not retained: %+v err=%v", rejected.Plan, err)
	}
	second, err := coordinator.AutoPlan(ctx, AutoPlanInput{TaskID: uiTask.ID, ExpectedTaskRevision: rejected.Revision,
		ProviderID: profile.ID, Actor: "planner"})
	if err != nil || second.Task.Plan.Revision != 2 || second.Task.Plan.ReviewStatus != "pending" {
		t.Fatalf("revised UI plan = %+v err=%v", second.Task.Plan, err)
	}
	var revisionInput worker.PlanTask
	if err := json.Unmarshal([]byte(adapter.plannerRequest.Messages[1].Content), &revisionInput); err != nil {
		t.Fatal(err)
	}
	if revisionInput.ExperiencePreview != "A summary and edit action appear" ||
		revisionInput.ReviewFeedback != "Show the mobile recovery state before implementation" ||
		len(revisionInput.PreviousPlanSteps) != 1 || revisionInput.PreviousPlanSteps[0].Title != "Fix" {
		t.Fatalf("planner lost UX review context: %+v", revisionInput)
	}
}

type proposalAdapter struct {
	calls           int
	reviewCalls     int
	plannerCalls    int
	plannerRequest  providers.ChatRequest
	proposalRequest providers.ChatRequest
	selectionCalls  int
}

type rollbackFailureAdapter struct{}

func (rollbackFailureAdapter) StreamChat(_ context.Context, _ providers.Profile, _ string, _ providers.ChatRequest,
	_ func(providers.Delta) error) (providers.Completion, error) {
	return providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{
		Name: "submit_changes", Arguments: `{"summary":"correct README status","recommended_checks":["node test.js"],"changes":[{"path":"README.md","edits":[{"old":"Status: outdated","new":"Status: ready"}]}]}`,
	}}}, nil
}

func TestFailedMandatoryTaskCheckPersistsRollbackReceiptInActivity(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	root := t.TempDir()
	original := "Status: outdated\n"
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "test.js"), []byte("process.exit(1)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	products := product.NewService(dataStore, skills.NewService(dataStore))
	defer products.Close()
	project, err := products.EnsureWorkspaceProject(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	providersService := providers.NewService(dataStore, rollbackFailureAdapter{})
	profile, err := providersService.Save(ctx, providers.SaveInput{Name: "fixture worker", BaseURL: "http://127.0.0.1:8088/v1",
		Model: "fixture-model", ContextWindow: 98304, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	tasks := taskengine.NewService(dataStore)
	task, err := tasks.Create(ctx, taskengine.CreateTaskInput{ProjectID: project.ID, Title: "Rollback fixture",
		Objective: "correct the README status", OriginalRequest: "change the status", Actor: "owner",
		Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "README status is ready and mandatory test passes"}},
		Contract: taskengine.GoalContract{WritablePaths: []string{"README.md"}}})
	if err != nil {
		t.Fatal(err)
	}
	task, err = tasks.CreatePlan(ctx, taskengine.CreatePlanInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		RequirementRevision: task.ActiveRequirementRevision, Reason: "bounded fixture", Actor: "planner",
		Steps: []taskengine.StepSpec{{Key: "fix", Title: "Correct README", Instructions: "change only the status",
			RequirementIDs: []string{"AC-1"}, Checks: []string{"node test.js"},
			EffectScope: []string{proposalEffect, "workspace.apply", "workspace.run", "provider.review"}}}})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := tasks.BuildNextStepPacket(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := tasks.BeginRun(ctx, taskengine.BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		Owner: "coordinator", LeaseDuration: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	task, err = tasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := tasks.BeginStepAttempt(ctx, taskengine.BeginAttemptInput{RunID: run.ID, LeaseToken: run.LeaseToken,
		StepKey: "fix", ExpectedTaskRevision: task.Revision, ExpectedStepRevision: task.Plan.Steps[0].Revision,
		InputHash: packet.CanonicalPacketHash, Packet: &packet})
	if err != nil {
		t.Fatal(err)
	}
	authority := taskengine.RunAuthority{RunID: run.ID, LeaseToken: run.LeaseToken}
	coordinator := New(tasks, products, providersService)
	proposed, err := coordinator.Propose(ctx, ProposalInput{AttemptID: attempt.ID, ProviderID: profile.ID,
		Authority: authority, Packet: packet, Files: []string{"README.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.DecideCodeProposal(ctx, proposed.Proposal.ID, "reviewer", taskengine.ProposalApproved,
		"bounded change matches the original README", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Apply(ctx, authority, proposed.Proposal.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	verified, verifyErr := coordinator.VerifyFrozen(ctx, authority, proposed.Proposal.ID, "owner")
	if verifyErr == nil || !verified.RolledBack || verified.RollbackEvidence.Kind != "code_rollback_receipt" ||
		verified.Proposal.State != taskengine.ProposalVerifyFailed {
		t.Fatalf("verification=%+v err=%v", verified, verifyErr)
	}
	current, err := products.ReadProjectFile(ctx, project.ID, "README.md")
	if err != nil || current.Content != original {
		t.Fatalf("rollback file=%+v err=%v", current, err)
	}
	_, receiptBody, err := products.GetArtifact(ctx, verified.RollbackEvidence.ID)
	if err != nil {
		t.Fatal(err)
	}
	var receipt rollbackReceipt
	if err := json.Unmarshal(receiptBody, &receipt); err != nil || !receipt.Restored ||
		len(receipt.Files) != 1 || receipt.Files[0].Status != "restored" || receipt.Files[0].AfterSHA256 != current.SHA256 {
		t.Fatalf("rollback receipt=%+v err=%v", receipt, err)
	}
	activity, err := tasks.ListActivity(ctx, task.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	linked := false
	for _, item := range activity {
		if item.Kind == "failure" {
			for _, ref := range item.EvidenceRefs {
				if ref == "rollback_artifact:"+verified.RollbackEvidence.ID {
					linked = true
				}
			}
		}
	}
	if !linked {
		t.Fatalf("rollback receipt missing from Task history: %+v", activity)
	}
	packet, err = tasks.BuildNextStepPacket(ctx, task.ID)
	if err != nil {
		t.Fatalf("rebuild packet: %v", err)
	}
	items, err := coordinator.proposalFailureEvidence(ctx, packet)
	if err != nil {
		t.Fatalf("proposal failure evidence: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected exactly 1 failure evidence record, got %d", len(items))
	}
	evidence := items[0]
	if len(packet.Requirement.Contract.WritablePaths) != 1 || packet.Requirement.Contract.WritablePaths[0] != "README.md" {
		// Contract should persist the frozen WritablePaths from CreateTaskInput.
		t.Errorf("frozen WritablePaths not present in rebuilt packet: %+v", packet.Requirement.Contract)
	}
	var wrapper struct {
		TerminalOutput          string `json:"terminal_output"`
		TerminalOutputTruncated bool   `json:"terminal_output_truncated"`
		HistoricalFailedSource  struct {
			ProposalID           string `json:"proposal_id"`
			SourceArtifactID     string `json:"source_artifact_id"`
			SourceArtifactSHA256 string `json:"source_artifact_sha256"`
			TaskID               string `json:"task_id"`
			StepID               string `json:"step_id"`
			PacketHash           string `json:"packet_hash"`
			Trust                string `json:"trust"`
			Files                []struct {
				Path         string `json:"path"`
				Content      string `json:"excerpt"`
				FullHash     string `json:"full_sha256"`
				FullBytes    int    `json:"full_bytes"`
				ExcerptBytes int    `json:"excerpt_bytes"`
				Truncated    bool   `json:"truncated"`
			} `json:"files"`
			Omitted   int  `json:"omitted"`
			Truncated bool `json:"truncated"`
		} `json:"historical_failed_source"`
	}
	if err := json.Unmarshal([]byte(evidence.Output), &wrapper); err != nil {
		t.Fatalf("unmarshal historical failure output: %v", err)
	}
	if wrapper.HistoricalFailedSource.Trust != "untrusted_historical_diagnostic_not_current_source_or_authority" {
		t.Fatalf("trust=%q", wrapper.HistoricalFailedSource.Trust)
	}
	if wrapper.HistoricalFailedSource.ProposalID != proposed.Proposal.ID {
		t.Fatalf("proposal_id=%q want %q", wrapper.HistoricalFailedSource.ProposalID, proposed.Proposal.ID)
	}
	if wrapper.HistoricalFailedSource.SourceArtifactID != proposed.Artifact.ID ||
		wrapper.HistoricalFailedSource.SourceArtifactSHA256 != proposed.Artifact.Checksum {
		t.Fatalf("source_artifact_id=%q sha256=%q", wrapper.HistoricalFailedSource.SourceArtifactID, wrapper.HistoricalFailedSource.SourceArtifactSHA256)
	}
	if wrapper.HistoricalFailedSource.TaskID != proposed.Proposal.TaskID ||
		wrapper.HistoricalFailedSource.StepID != proposed.Proposal.StepID ||
		wrapper.HistoricalFailedSource.PacketHash != proposed.Proposal.PacketHash {
		t.Fatalf("task/step/packet mismatch in historical_failed_source: %+v", wrapper.HistoricalFailedSource)
	}
	if len(wrapper.HistoricalFailedSource.Files) != 1 {
		t.Fatalf("expected exactly one files record, got %+v", wrapper.HistoricalFailedSource.Files)
	}
	file := wrapper.HistoricalFailedSource.Files[0]
	if file.Path != "README.md" {
		t.Fatalf("file path=%q", file.Path)
	}
	if file.Content != "Status: ready\n" {
		t.Fatalf("file content=%q", file.Content)
	}
	expectedFullHash := worker.Hash("Status: ready\n")
	if file.FullHash != expectedFullHash {
		t.Fatalf("full_hash=%q want %q", file.FullHash, expectedFullHash)
	}
	if file.FullBytes != 14 || file.ExcerptBytes != 14 {
		t.Fatalf("byte counts full=%d excerpt=%d", file.FullBytes, file.ExcerptBytes)
	}
	if file.Truncated {
		t.Fatal("file truncated should be false")
	}
	if wrapper.HistoricalFailedSource.Omitted != 0 {
		t.Fatalf("historical omitted should be 0, got %d", wrapper.HistoricalFailedSource.Omitted)
	}
	if wrapper.HistoricalFailedSource.Truncated {
		t.Fatal("historical truncated should be false")
	}
	if current, err2 := products.ReadProjectFile(ctx, project.ID, "README.md"); err2 != nil || current.Content != original {
		t.Fatalf("current workspace README should remain original: %+v", current)
	}
	if err != nil {
		t.Fatalf("ReadProjectFile: %v", err)
	}
	artifact, terminalBody, readErr := products.GetArtifact(ctx, evidence.ArtifactID)
	if readErr != nil || artifact.Checksum != evidence.ArtifactSHA256 || artifact.ProjectID != project.ID || artifact.Kind != "terminal_log" {
		t.Fatalf("terminal artifact mismatch: %+v", artifact)
	}
	if string(terminalBody) != wrapper.TerminalOutput {
		t.Fatalf("terminal output should remain original: %s", terminalBody)
	}

	// Real replan regression: create a fresh task revision, propose a diagnostic
	// replan, and verify the proposal packet and failure evidence are stable.
	fresh, err := tasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("fresh task get: %v", err)
	}
	if fresh.Revision == 0 || fresh.ActiveRequirementRevision == 0 || fresh.ActivePlanRevision == 0 {
		t.Fatalf("fresh task revision fields zero: %+v", fresh)
	}
	newTask, err := tasks.CreatePlan(ctx, taskengine.CreatePlanInput{
		TaskID:               fresh.ID,
		ExpectedTaskRevision: fresh.Revision,
		RequirementRevision:  fresh.ActiveRequirementRevision,
		Actor:                "planner",
		Reason:               "replan diagnostic",
		Steps: []taskengine.StepSpec{{
			Key:            "fix-replanned",
			Title:          "Diagnostic replan step",
			Instructions:   "No-op diagnostic step for replan regression",
			RequirementIDs: []string{"AC-1"},
			Checks:         []string{"node test.js"},
			EffectScope:    []string{"provider.propose", "workspace.apply", "workspace.run", "provider.review"},
		}},
	})
	if err != nil {
		t.Fatalf("create replan plan: %v", err)
	}
	if newTask.ID != task.ID {
		t.Fatalf("replan should create a new task: %s", newTask.ID)
	}
	if newTask.ActivePlanRevision != 2 {
		t.Fatalf("replan plan revision should be 2, got %d", newTask.ActivePlanRevision)
	}
	if len(newTask.Plan.Steps) != 1 {
		t.Fatalf("replan plan should have exactly 1 step, got %d", len(newTask.Plan.Steps))
	}
	if newTask.Plan.Steps[0].ID == proposed.Proposal.StepID {
		t.Fatalf("replan step id should differ from original proposal step id")
	}
	newPacket, err := tasks.BuildNextStepPacket(ctx, newTask.ID)
	if err != nil {
		t.Fatalf("build next step packet: %v", err)
	}
	if newPacket.Step.ID == proposed.Proposal.StepID {
		t.Fatalf("new packet step id should differ from original proposal step id")
	}
	if newPacket.CanonicalPacketHash == proposed.Proposal.PacketHash {
		t.Fatalf("new packet hash should differ from original packet hash")
	}
	replanEvidence, err := coordinator.proposalFailureEvidence(ctx, newPacket)
	if err != nil {
		t.Fatalf("replan proposal failure evidence: %v", err)
	}
	if len(replanEvidence) != 1 {
		t.Fatalf("expected 1 replan failure evidence record, got %d", len(replanEvidence))
	}
	if replanEvidence[0].ArtifactID != evidence.ArtifactID || replanEvidence[0].ArtifactSHA256 != evidence.ArtifactSHA256 {
		t.Fatalf("replan failure evidence artifact mismatch: %+v", replanEvidence[0])
	}
	var envelope struct {
		TerminalOutput         string `json:"terminal_output"`
		HistoricalFailedSource struct {
			StepID     string `json:"step_id"`
			ProposalID string `json:"proposal_id"`
			PacketHash string `json:"packet_hash"`
		} `json:"historical_failed_source"`
	}
	if err := json.Unmarshal([]byte(replanEvidence[0].Output), &envelope); err != nil {
		t.Fatalf("decode replan historical failed source: %v", err)
	}
	if envelope.HistoricalFailedSource.StepID != proposed.Proposal.StepID ||
		envelope.HistoricalFailedSource.ProposalID != proposed.Proposal.ID ||
		envelope.HistoricalFailedSource.PacketHash != proposed.Proposal.PacketHash ||
		envelope.TerminalOutput != string(terminalBody) {
		t.Fatalf("replan historical failed source mismatch: %+v", envelope)
	}
	readme, err := products.ReadProjectFile(ctx, project.ID, "README.md")
	if err != nil {
		t.Fatalf("read README after replan: %v", err)
	}
	if readme.Content != original {
		t.Fatalf("README should remain original after replan")
	}
}

type rollbackReviewAdapter struct{ onReview func(providers.ChatRequest) }

func (a rollbackReviewAdapter) StreamChat(ctx context.Context, profile providers.Profile, key string, request providers.ChatRequest,
	stream func(providers.Delta) error) (providers.Completion, error) {
	if len(request.Tools) == 1 && request.Tools[0].Function.Name == "submit_review" {
		if a.onReview != nil {
			a.onReview(request)
		}
		return providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{
			Name: "submit_review", Arguments: `{"verdict":"reject","rationale":"independent reviewer found an unresolved issue","findings":["fixture reviewer rejection"]}`,
		}}}, nil
	}
	return rollbackFailureAdapter{}.StreamChat(ctx, profile, key, request, stream)
}

func TestPostReviewRejectionPersistsRollbackReceiptInActivity(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	root := t.TempDir()
	original := "Status: outdated\n"
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	passingCheck := "const fs=require('fs'); if(!fs.readFileSync('README.md','utf8').includes('Status: ready')) process.exit(2);\n"
	if err := os.WriteFile(filepath.Join(root, "test.js"), []byte(passingCheck), 0o600); err != nil {
		t.Fatal(err)
	}
	products := product.NewService(dataStore, skills.NewService(dataStore))
	defer products.Close()
	project, err := products.EnsureWorkspaceProject(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	reviewCaptured := false
	providersService := providers.NewService(dataStore, rollbackReviewAdapter{onReview: func(request providers.ChatRequest) {
		var input worker.ReviewTask
		if err := json.Unmarshal([]byte(request.Messages[1].Content), &input); err != nil {
			t.Fatal(err)
		}
		if len(input.AcceptanceCriteria) != 1 || !strings.HasPrefix(input.AcceptanceCriteria[0], "AC-1:") {
			t.Fatalf("review got future step criteria: %+v", input.AcceptanceCriteria)
		}
		reviewCaptured = true
	}})
	workerProfile, err := providersService.Save(ctx, providers.SaveInput{Name: "fixture worker", BaseURL: "http://127.0.0.1:8088/v1",
		Model: "worker-fixture", ContextWindow: 98304, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	reviewerProfile, err := providersService.Save(ctx, providers.SaveInput{Name: "fixture reviewer", BaseURL: "http://127.0.0.1:8088/v1",
		Model: "reviewer-fixture", ContextWindow: 98304, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	tasks := taskengine.NewService(dataStore)
	task, err := tasks.Create(ctx, taskengine.CreateTaskInput{ProjectID: project.ID, Title: "Post-review rollback fixture",
		Objective: "correct the README status", OriginalRequest: "change the status", Actor: "owner",
		Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "README status is ready and mandatory test passes"}, {ID: "AC-2", Description: "Future release must also be verified"}}})
	if err != nil {
		t.Fatal(err)
	}
	task, err = tasks.CreatePlan(ctx, taskengine.CreatePlanInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		RequirementRevision: task.ActiveRequirementRevision, Reason: "bounded fixture", Actor: "planner",
		Steps: []taskengine.StepSpec{{Key: "fix", Title: "Correct README", Instructions: "change only the status",
			RequirementIDs: []string{"AC-1"}, Checks: []string{"node test.js"},
			EffectScope: []string{proposalEffect, "workspace.apply", "workspace.run", "provider.review"}},
			{Key: "release", Title: "Verify future release", Instructions: "Verify release separately", RequirementIDs: []string{"AC-2"}, Dependencies: []string{"fix"}, Checks: []string{"node test.js"}, EffectScope: []string{proposalEffect, "workspace.apply", "workspace.run", "provider.review"}}}})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := tasks.BuildNextStepPacket(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := tasks.BeginRun(ctx, taskengine.BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		Owner: "coordinator", LeaseDuration: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	task, err = tasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := tasks.BeginStepAttempt(ctx, taskengine.BeginAttemptInput{RunID: run.ID, LeaseToken: run.LeaseToken,
		StepKey: "fix", ExpectedTaskRevision: task.Revision, ExpectedStepRevision: task.Plan.Steps[0].Revision,
		InputHash: packet.CanonicalPacketHash, Packet: &packet})
	if err != nil {
		t.Fatal(err)
	}
	authority := taskengine.RunAuthority{RunID: run.ID, LeaseToken: run.LeaseToken}
	coordinator := New(tasks, products, providersService)
	proposed, err := coordinator.Propose(ctx, ProposalInput{AttemptID: attempt.ID, ProviderID: workerProfile.ID,
		Authority: authority, Packet: packet, Files: []string{"README.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.DecideCodeProposal(ctx, proposed.Proposal.ID, "reviewer", taskengine.ProposalApproved,
		"bounded change matches the original README", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Apply(ctx, authority, proposed.Proposal.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	verified, err := coordinator.VerifyFrozen(ctx, authority, proposed.Proposal.ID, "owner")
	if err != nil || verified.Proposal.State != taskengine.ProposalAwaitingReview || verified.RolledBack {
		t.Fatalf("expected passing checks before reviewer rejection: %+v err=%v", verified, err)
	}
	reviewed, err := coordinator.Review(ctx, authority, proposed.Proposal.ID, reviewerProfile.ID)
	if !reviewCaptured {
		t.Fatal("reviewer was not dispatched")
	}
	if err != nil || reviewed.Review.Verdict != "reject" || !reviewed.RolledBack ||
		reviewed.RollbackEvidence.Kind != "code_rollback_receipt" || reviewed.Proposal.State != taskengine.ProposalReviewRejected {
		t.Fatalf("post-review rejection=%+v err=%v", reviewed, err)
	}
	current, err := products.ReadProjectFile(ctx, project.ID, "README.md")
	if err != nil || current.Content != original {
		t.Fatalf("rollback file=%+v err=%v", current, err)
	}
	_, receiptBody, err := products.GetArtifact(ctx, reviewed.RollbackEvidence.ID)
	if err != nil {
		t.Fatal(err)
	}
	var receipt rollbackReceipt
	if err := json.Unmarshal(receiptBody, &receipt); err != nil || !receipt.Restored || len(receipt.Files) != 1 ||
		receipt.Files[0].Status != "restored" || receipt.Files[0].AfterSHA256 != current.SHA256 {
		t.Fatalf("rollback receipt=%+v err=%v", receipt, err)
	}
	activity, err := tasks.ListActivity(ctx, task.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	linked := false
	for _, item := range activity {
		if item.Kind != "failure" {
			continue
		}
		for _, ref := range item.EvidenceRefs {
			if ref == "rollback_artifact:"+reviewed.RollbackEvidence.ID {
				linked = true
			}
		}
	}
	if !linked {
		t.Fatalf("post-review rollback receipt missing from Task history: %+v", activity)
	}
}

func (a *proposalAdapter) StreamChat(_ context.Context, _ providers.Profile, _ string, request providers.ChatRequest,
	_ func(providers.Delta) error) (providers.Completion, error) {
	if len(request.Tools) == 1 && request.Tools[0].Function.Name == "submit_review" {
		a.reviewCalls++
		return providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{
			Name: "submit_review", Arguments: `{"verdict":"approve","rationale":"the applied source and actual test evidence cover AC-1","findings":[]}`,
		}}}, nil
	}
	if len(request.Tools) == 1 && request.Tools[0].Function.Name == "submit_plan" {
		a.plannerCalls++
		a.plannerRequest = request
		return providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: "submit_plan", Arguments: `{
          "reason":"bounded fix","steps":[{"key":"fix","title":"Fix","workspace_change":"Correct the implementation","instructions":"correct the implementation","requirement_ids":["AC-1"],"dependencies":[],"checks":["go test ./..."],"effect_scope":["provider.select_files","provider.propose","workspace.apply","workspace.run","provider.review"]}]}`}}}, nil
	}
	if len(request.Tools) == 1 && request.Tools[0].Function.Name == "submit_file_selection" {
		a.selectionCalls++
		return providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{
			Name: "submit_file_selection", Arguments: `{"files":["sum.go","sum_test.go"],"rationale":"implementation and focused test"}`,
		}}}, nil
	}
	a.calls++
	a.proposalRequest = request
	return providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{
		Name: "submit_changes", Arguments: `{"summary":"fix addition","recommended_checks":["go test ./..."],"changes":[{"path":"sum.go","edits":[{"old":"return a-b","new":"return a+b"}]}]}`,
	}}}, nil
}

func TestProposalWorkerPersistsReviewArtifactWithoutWritingSource(t *testing.T) {
	testProposalReviewWorkflow(t, false)
}

func TestPostReviewWaitsForHumanAcceptanceWithoutReportingFailure(t *testing.T) {
	testProposalReviewWorkflow(t, true)
}

func testProposalReviewWorkflow(t *testing.T, requireAcceptance bool) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	projectRoot := t.TempDir()
	before := "package sample\n\nfunc add(a, b int) int { return a-b }\n"
	if err = os.WriteFile(filepath.Join(projectRoot, "sum.go"), []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(projectRoot, "go.mod"), []byte("module sample\n\ngo 1.25\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(projectRoot, "sum_test.go"), []byte("package sample\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { if add(2, 3) != 5 { t.Fatal(\"bad sum\") } }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	productService := product.NewService(dataStore, skills.NewService(dataStore))
	defer productService.Close()
	project, err := productService.EnsureWorkspaceProject(ctx, projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &proposalAdapter{}
	t.Setenv("TASKCOORD_TEST_KEY", "credential-that-must-not-leave")
	providerService := providers.NewService(dataStore, adapter)
	profile, err := providerService.Save(ctx, providers.SaveInput{
		Name: "worker", BaseURL: "https://worker.example/v1", Model: "qwen-test", APIKeyEnv: "TASKCOORD_TEST_KEY",
		ContextWindow: 98304, MaxOutputTokens: 8192,
	})
	if err != nil {
		t.Fatal(err)
	}
	reviewerProfile, err := providerService.Save(ctx, providers.SaveInput{
		Name: "same-model reviewer", BaseURL: "https://worker.example/v1", Model: "qwen-test", APIKeyEnv: "TASKCOORD_TEST_KEY",
		ContextWindow: 16384, MaxOutputTokens: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	tasks := taskengine.NewService(dataStore)
	task, err := tasks.Create(ctx, taskengine.CreateTaskInput{
		ProjectID: project.ID, Title: "Fix sum", Objective: "make addition correct", OriginalRequest: "fix add",
		Constraints: []string{"preserve function signature"},
		Criteria:    []taskengine.Criterion{{ID: "AC-1", Description: "addition returns the sum"}}, Actor: "owner",
		Contract:     taskengine.GoalContract{RequireUserAcceptance: requireAcceptance, WritablePaths: []string{"sum.go"}},
		EgressPolicy: "remote_allowed", RemoteEgressApproval: &taskengine.TaskEgressApproval{Actor: "owner", Reason: "test adapter"},
	})
	if err != nil {
		t.Fatal(err)
	}
	brain := &fakeBrain{fragments: []ctxcompiler.Fragment{brainFragment("team-alpha")}}
	coordinator := New(tasks, productService, providerService).WithProjectBrain(project.ID, "team-alpha", brain,
		ctxcompiler.NewCompiler(ctxcompiler.NewAdaptiveEstimator(), nil, nil))
	planned, err := coordinator.AutoPlan(ctx, AutoPlanInput{TaskID: task.ID,
		ExpectedTaskRevision: task.Revision, ProviderID: profile.ID, Actor: "planner"})
	if err != nil || planned.Run.State != taskengine.PlannerObserved || planned.Artifact.Kind != "task_plan_proposal" ||
		planned.Task.State != taskengine.StateReady || len(planned.Task.Plan.Steps) != 1 ||
		len(planned.Task.Plan.Steps[0].RequirementIDs) != 1 || planned.Task.Plan.Steps[0].RequirementIDs[0] != "AC-1" || adapter.plannerCalls != 1 {
		t.Fatalf("planned=%+v calls=%d err=%v", planned, adapter.plannerCalls, err)
	}
	var plannerInput worker.PlanTask
	if err = json.Unmarshal([]byte(adapter.plannerRequest.Messages[1].Content), &plannerInput); err != nil ||
		len(plannerInput.ProjectBrainRefs) != 1 || plannerInput.ProjectBrainRefs[0].Version != brain.fragments[0].Version {
		t.Fatalf("durable planner did not receive versioned reference: input=%+v err=%v", plannerInput, err)
	}
	if planned.Artifact.Metadata["planning_packet_hash"] != worker.Hash(adapter.plannerRequest.Messages[1].Content) {
		t.Fatalf("planner reference not covered by durable input hash: %+v", planned.Artifact.Metadata)
	}
	task = planned.Task
	packet, err := tasks.BuildNextStepPacket(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = taskengine.ValidateStepPacket(packet); err != nil {
		t.Fatalf("Project Brain changed authoritative step packet: %v", err)
	}
	encodedPacket, _ := json.Marshal(packet)
	if strings.Contains(string(encodedPacket), "project_brain_refs") {
		t.Fatal("external reference entered the authoritative step packet")
	}
	run, err := tasks.BeginRun(ctx, taskengine.BeginRunInput{
		TaskID: task.ID, ExpectedTaskRevision: task.Revision, Owner: "coordinator", LeaseDuration: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err = tasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := tasks.BeginStepAttempt(ctx, taskengine.BeginAttemptInput{
		RunID: run.ID, LeaseToken: run.LeaseToken, StepKey: "fix", ExpectedTaskRevision: task.Revision,
		ExpectedStepRevision: task.Plan.Steps[0].Revision, InputHash: packet.CanonicalPacketHash, Packet: &packet,
	})
	if err != nil {
		t.Fatal(err)
	}
	authority := taskengine.RunAuthority{RunID: run.ID, LeaseToken: run.LeaseToken}
	selection, err := coordinator.SelectFiles(ctx, SelectFilesInput{AttemptID: attempt.ID, ProviderID: profile.ID, Authority: authority})
	if err != nil || adapter.selectionCalls != 1 || selection.Artifact.Kind != "task_file_selection" ||
		selection.Effect.State != taskengine.EffectObserved || len(selection.Result.Files) != 2 {
		t.Fatalf("selection=%+v calls=%d err=%v", selection, adapter.selectionCalls, err)
	}
	reused, err := coordinator.SelectFiles(ctx, SelectFilesInput{AttemptID: attempt.ID, ProviderID: profile.ID, Authority: authority})
	if err != nil || adapter.selectionCalls != 1 || reused.Artifact.ID != selection.Artifact.ID {
		t.Fatalf("reused selection=%+v calls=%d err=%v", reused, adapter.selectionCalls, err)
	}
	if _, err = dataStore.DB.ExecContext(ctx, `UPDATE task_effect_intents SET state='uncertain' WHERE operation_id=?`, selection.Effect.OperationID); err != nil {
		t.Fatal(err)
	}
	recoveredSelection, err := coordinator.ReconcileProviderArtifact(ctx, selection.Effect.OperationID)
	if err != nil || recoveredSelection.Outcome != "artifact_recovered" || recoveredSelection.Effect.State != taskengine.EffectReconciled {
		t.Fatalf("recovered selection=%+v err=%v", recoveredSelection, err)
	}
	reused, err = coordinator.SelectFiles(ctx, SelectFilesInput{AttemptID: attempt.ID, ProviderID: profile.ID, Authority: authority})
	if err != nil || adapter.selectionCalls != 1 || reused.Artifact.ID != selection.Artifact.ID {
		t.Fatalf("reconciled selection was replayed: %+v calls=%d err=%v", reused, adapter.selectionCalls, err)
	}
	output, err := coordinator.Propose(ctx, ProposalInput{
		AttemptID: attempt.ID, ProviderID: profile.ID, Authority: authority, Files: selection.Result.Files,
	})
	if err != nil {
		t.Fatal(err)
	}
	if adapter.calls != 1 || output.Result.Status != "proposed_unverified" || output.Artifact.Kind != "code_proposal" || output.Effect.State != taskengine.EffectObserved || output.Proposal.State != taskengine.ProposalPendingReview {
		t.Fatalf("proposal output=%+v calls=%d", output, adapter.calls)
	}
	var proposalInput worker.Task
	if err = json.Unmarshal([]byte(adapter.proposalRequest.Messages[1].Content), &proposalInput); err != nil ||
		len(proposalInput.ProjectBrainRefs) != 1 || proposalInput.ProjectBrainRefs[0].Trust != "external_curated_not_verified" ||
		output.Result.InputSHA256 != worker.Hash(adapter.proposalRequest.Messages[1].Content) {
		t.Fatalf("proposal lacked bound external reference: input=%+v result=%+v err=%v", proposalInput, output.Result, err)
	}
	beforeReplayReads := brain.calls
	brain.err = fmt.Errorf("Pi offline after observed effect")
	reusedProposal, err := coordinator.Propose(ctx, ProposalInput{AttemptID: attempt.ID, ProviderID: profile.ID, Authority: authority, Files: selection.Result.Files})
	if err != nil || adapter.calls != 1 || brain.calls != beforeReplayReads || reusedProposal.Proposal.ID != output.Proposal.ID || reusedProposal.Artifact.ID != output.Artifact.ID {
		t.Fatalf("proposal was replayed instead of reused: %+v calls=%d err=%v", reusedProposal, adapter.calls, err)
	}
	if _, err = New(tasks, productService, providerService).Apply(ctx, authority, output.Proposal.ID, "owner"); err == nil {
		t.Fatal("unreviewed proposal was applied")
	}
	approved, err := tasks.DecideCodeProposal(ctx, output.Proposal.ID, "astra-reviewer", taskengine.ProposalApproved,
		"preimage and bounded change are correct; tests are still required after apply", []string{"no out-of-scope files"})
	if err != nil || approved.State != taskengine.ProposalApproved {
		t.Fatalf("approved proposal=%+v err=%v", approved, err)
	}
	if _, err = tasks.DecideCodeProposal(ctx, output.Proposal.ID, "second-reviewer", taskengine.ProposalRejected,
		"late conflicting decision", nil); err == nil {
		t.Fatal("proposal accepted a second review decision")
	}
	after, err := os.ReadFile(filepath.Join(projectRoot, "sum.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != before {
		t.Fatalf("proposal worker modified source: %q", after)
	}
	_, artifactBody, err := productService.GetArtifact(ctx, output.Artifact.ID)
	if err != nil || len(artifactBody) == 0 {
		t.Fatalf("proposal artifact missing: bytes=%d err=%v", len(artifactBody), err)
	}
	applied, err := New(tasks, productService, providerService).Apply(ctx, authority, output.Proposal.ID, "owner")
	if err != nil || applied.Proposal.State != taskengine.ProposalApplied || applied.Proposal.RollbackArtifactID != applied.RollbackArtifact.ID ||
		applied.Effect.State != taskengine.EffectObserved || len(applied.Receipts) != 1 {
		t.Fatalf("apply output=%+v err=%v", applied, err)
	}
	after, err = os.ReadFile(filepath.Join(projectRoot, "sum.go"))
	if err != nil || string(after) != "package sample\n\nfunc add(a, b int) int { return a+b }\n" {
		t.Fatalf("applied source=%q err=%v", after, err)
	}
	if _, err = New(tasks, productService, providerService).Verify(ctx, authority, output.Proposal.ID, "owner", []CommandCheck{{
		ID: "go test ./...", Executable: "go", Arguments: []string{"test", "./..."}, WorkingDir: ".", TimeoutSeconds: 60,
	}}); err == nil {
		t.Fatal("final-step verification ran without acceptance-criterion coverage")
	}
	verified, err := New(tasks, productService, providerService).VerifyFrozen(ctx, authority, output.Proposal.ID, "owner")
	if err != nil || verified.Proposal.State != taskengine.ProposalAwaitingReview || verified.Proposal.VerificationArtifactID != verified.Evidence.ID ||
		verified.RolledBack || len(verified.Jobs) != 1 || len(verified.Validations) != 1 || verified.Evidence.Kind != "code_verification_bundle" {
		t.Fatalf("verify output=%+v err=%v", verified, err)
	}
	if _, err = dataStore.DB.ExecContext(ctx, `UPDATE task_effect_intents SET state='uncertain' WHERE operation_id=?`, verified.Effects[0].OperationID); err != nil {
		t.Fatal(err)
	}
	reconciled, err := New(tasks, productService, providerService).ReconcileWorkspaceRun(ctx, verified.Effects[0].OperationID)
	if err != nil || reconciled.Effect.State != taskengine.EffectReconciled || reconciled.Job == nil || reconciled.Job.ID != verified.Jobs[0].ID {
		t.Fatalf("reconciled=%+v err=%v", reconciled, err)
	}
	if _, err = New(tasks, productService, providerService).Review(ctx, authority, output.Proposal.ID, profile.ID); err == nil {
		t.Fatal("implementer provider was allowed to review its own proposal")
	}
	// A distinct profile on the same endpoint/model performs a fresh, auditable
	// second pass when a local installation has only one model.
	for index := 0; index < 1000; index++ {
		if _, err = productService.CreateArtifact(ctx, product.ArtifactInput{ProjectID: project.ID,
			Name: fmt.Sprintf("unrelated-%04d.txt", index), Kind: "unrelated", MIMEType: "text/plain", Content: "noise"}); err != nil {
			t.Fatal(err)
		}
	}
	// A pause/resume changes only the step lifecycle revision. Its frozen
	// command evidence remains bound to this same verified proposal.
	if _, err = dataStore.DB.ExecContext(ctx, `UPDATE task_steps SET revision=revision+2 WHERE id=?`, task.Plan.Steps[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err = dataStore.DB.ExecContext(ctx, `UPDATE task_step_attempts SET step_revision=step_revision+2 WHERE id=?`, attempt.ID); err != nil {
		t.Fatal(err)
	}
	reviewed, err := New(tasks, productService, providerService).Review(ctx, authority, output.Proposal.ID, reviewerProfile.ID)
	if err != nil || reviewed.Proposal.State != taskengine.ProposalVerified || reviewed.Review.Verdict != "approve" || adapter.reviewCalls != 1 {
		t.Fatalf("reviewed output=%+v calls=%d err=%v", reviewed, adapter.reviewCalls, err)
	}
	finishedTask, err := tasks.Get(ctx, task.ID)
	if requireAcceptance {
		if err != nil || finishedTask.State != taskengine.StateVerifying {
			t.Fatalf("review must leave the task waiting for acceptance: task=%+v err=%v", finishedTask, err)
		}
		if _, err := tasks.RecoverInterrupted(ctx); err != nil {
			t.Fatal(err)
		}
		finishedTask, err = tasks.Get(ctx, task.ID)
		if err != nil || finishedTask.State != taskengine.StateVerifying {
			t.Fatalf("restart hid the acceptance action: task=%+v err=%v", finishedTask, err)
		}
		report, err := tasks.EvaluateCompletion(ctx, task.ID, finishedTask.Revision)
		if err != nil || !report.AwaitingUserAcceptance || report.CanComplete {
			t.Fatalf("acceptance gate=%+v err=%v", report, err)
		}
		if _, err := tasks.CompleteTask(ctx, task.ID, finishedTask.Revision); err == nil {
			t.Fatal("review must not bypass human acceptance")
		}
		if _, err := tasks.RecordUserAcceptance(ctx, task.ID, finishedTask.Revision, report.SubjectHash, "test-human"); err != nil {
			t.Fatal(err)
		}
		finishedTask, err = tasks.CompleteTask(ctx, task.ID, finishedTask.Revision)
	}
	if err != nil || finishedTask.State != taskengine.StateCompleted || finishedTask.Plan.Steps[0].State != taskengine.StepCompleted {
		t.Fatalf("finished task=%+v err=%v", finishedTask, err)
	}
}

func TestParseFrozenCommandRejectsShellAndPreservesQuotedArguments(t *testing.T) {
	args, err := parseFrozenCommand(`go test "./folder with spaces/..."`)
	if err != nil || len(args) != 3 || args[0] != "go" || args[1] != "test" || args[2] != "./folder with spaces/..." {
		t.Fatalf("args=%q err=%v", args, err)
	}
	for _, command := range []string{"go test ./... && echo unsafe", "go test ./...\nnpm test", `go test "unterminated`} {
		if _, err = parseFrozenCommand(command); err == nil {
			t.Fatalf("unsafe frozen command was accepted: %q", command)
		}
	}
}

func TestProposalWorkerRejectsProviderCredentialBeforeDispatch(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	projectRoot := t.TempDir()
	credential := "credential-that-must-not-leave"
	if err = os.WriteFile(filepath.Join(projectRoot, "leak.txt"), []byte(credential), 0o600); err != nil {
		t.Fatal(err)
	}
	productService := product.NewService(dataStore, skills.NewService(dataStore))
	defer productService.Close()
	project, err := productService.EnsureWorkspaceProject(ctx, projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &proposalAdapter{}
	t.Setenv("TASKCOORD_SECRET_KEY", credential)
	providerService := providers.NewService(dataStore, adapter)
	profile, err := providerService.Save(ctx, providers.SaveInput{Name: "worker", BaseURL: "https://worker.example/v1",
		Model: "qwen-test", APIKeyEnv: "TASKCOORD_SECRET_KEY", ContextWindow: 98304, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	tasks := taskengine.NewService(dataStore)
	task, err := tasks.Create(ctx, taskengine.CreateTaskInput{ProjectID: project.ID, Title: "Secret guard", Objective: "do not leak",
		OriginalRequest: "inspect selected file", Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "credential stays local"}}, Actor: "owner",
		EgressPolicy: "remote_allowed", RemoteEgressApproval: &taskengine.TaskEgressApproval{Actor: "owner", Reason: "test credential guard"}})
	if err != nil {
		t.Fatal(err)
	}
	task, err = tasks.CreatePlan(ctx, taskengine.CreatePlanInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		RequirementRevision: task.ActiveRequirementRevision, Reason: "guard", Actor: "planner",
		Steps: []taskengine.StepSpec{{Key: "inspect", Title: "Inspect", Instructions: "inspect", RequirementIDs: []string{"AC-1"}, Checks: []string{"credential-local"}, EffectScope: []string{proposalEffect}}}})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := tasks.BuildNextStepPacket(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := tasks.BeginRun(ctx, taskengine.BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, Owner: "coordinator"})
	if err != nil {
		t.Fatal(err)
	}
	task, _ = tasks.Get(ctx, task.ID)
	attempt, err := tasks.BeginStepAttempt(ctx, taskengine.BeginAttemptInput{RunID: run.ID, LeaseToken: run.LeaseToken,
		StepKey: "inspect", ExpectedTaskRevision: task.Revision, ExpectedStepRevision: task.Plan.Steps[0].Revision, InputHash: packet.CanonicalPacketHash})
	if err != nil {
		t.Fatal(err)
	}
	authority := taskengine.RunAuthority{RunID: run.ID, LeaseToken: run.LeaseToken}
	output, err := New(tasks, productService, providerService).Propose(ctx, ProposalInput{
		AttemptID: attempt.ID, ProviderID: profile.ID, Authority: authority, Packet: packet, Files: []string{"leak.txt"},
	})
	if err == nil || adapter.calls != 0 || output.Effect.State != taskengine.EffectAbandoned {
		t.Fatalf("credential guard err=%v calls=%d effect=%+v", err, adapter.calls, output.Effect)
	}
}

func TestTaskProviderBindingRejectsRevisionDriftBeforeDispatch(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	providerService := providers.NewService(dataStore, &proposalAdapter{})
	profile, err := providerService.Save(ctx, providers.SaveInput{Name: "worker", BaseURL: "https://worker.example/v1",
		Model: "model-v1", ContextWindow: 32768, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	coordinator := New(nil, nil, providerService)
	if err = coordinator.validateProviderBinding(ctx, profile); err != nil {
		t.Fatalf("unchanged provider contract was rejected: %v", err)
	}
	if _, err = providerService.Save(ctx, providers.SaveInput{ID: profile.ID, Name: "worker", BaseURL: "https://worker.example/v1",
		Model: "model-v2", ContextWindow: 32768, MaxOutputTokens: 4096}); err != nil {
		t.Fatal(err)
	}
	if err = coordinator.validateProviderBinding(ctx, profile); err == nil {
		t.Fatal("task provider revision drift was accepted before dispatch")
	}
}
