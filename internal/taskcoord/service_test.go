package taskcoord

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	planned, err := New(tasks, products, providerService).AutoPlan(ctx, AutoPlanInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, ProviderID: profile.ID, Actor: "owner"})
	if err != nil || planned.Task.State != taskengine.StateReady || adapter.plannerCalls != 1 {
		t.Fatalf("61k plan failed: task=%+v calls=%d err=%v", planned.Task, adapter.plannerCalls, err)
	}
	if len(adapter.plannerRequest.Messages) != 2 {
		t.Fatalf("planner request messages=%d", len(adapter.plannerRequest.Messages))
	}
	var plannerInput worker.PlanTask
	if err = json.Unmarshal([]byte(adapter.plannerRequest.Messages[1].Content), &plannerInput); err != nil {
		t.Fatal(err)
	}
	if len(plannerInput.RepositoryFiles) != 2 || plannerInput.RepositoryFiles[0].Path != "go.mod" ||
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
}

type proposalAdapter struct {
	calls          int
	reviewCalls    int
	plannerCalls   int
	plannerRequest providers.ChatRequest
	selectionCalls int
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
	return providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{
		Name: "submit_changes", Arguments: `{"summary":"fix addition","recommended_checks":["go test ./..."],"changes":[{"path":"sum.go","edits":[{"old":"return a-b","new":"return a+b"}]}]}`,
	}}}, nil
}

func TestProposalWorkerPersistsReviewArtifactWithoutWritingSource(t *testing.T) {
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
		Name: "reviewer", BaseURL: "https://review.example/v1", Model: "review-test", APIKeyEnv: "TASKCOORD_TEST_KEY",
		ContextWindow: 98304, MaxOutputTokens: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	tasks := taskengine.NewService(dataStore)
	task, err := tasks.Create(ctx, taskengine.CreateTaskInput{
		ProjectID: project.ID, Title: "Fix sum", Objective: "make addition correct", OriginalRequest: "fix add",
		Constraints: []string{"preserve function signature"},
		Criteria:    []taskengine.Criterion{{ID: "AC-1", Description: "addition returns the sum"}}, Actor: "owner",
		EgressPolicy: "remote_allowed", RemoteEgressApproval: &taskengine.TaskEgressApproval{Actor: "owner", Reason: "test adapter"},
	})
	if err != nil {
		t.Fatal(err)
	}
	planned, err := New(tasks, productService, providerService).AutoPlan(ctx, AutoPlanInput{TaskID: task.ID,
		ExpectedTaskRevision: task.Revision, ProviderID: profile.ID, Actor: "planner"})
	if err != nil || planned.Run.State != taskengine.PlannerObserved || planned.Artifact.Kind != "task_plan_proposal" ||
		planned.Task.State != taskengine.StateReady || len(planned.Task.Plan.Steps) != 1 ||
		len(planned.Task.Plan.Steps[0].RequirementIDs) != 1 || planned.Task.Plan.Steps[0].RequirementIDs[0] != "AC-1" || adapter.plannerCalls != 1 {
		t.Fatalf("planned=%+v calls=%d err=%v", planned, adapter.plannerCalls, err)
	}
	task = planned.Task
	packet, err := tasks.BuildNextStepPacket(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
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
	coordinator := New(tasks, productService, providerService)
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
	reusedProposal, err := coordinator.Propose(ctx, ProposalInput{AttemptID: attempt.ID, ProviderID: profile.ID, Authority: authority, Files: selection.Result.Files})
	if err != nil || adapter.calls != 1 || reusedProposal.Proposal.ID != output.Proposal.ID || reusedProposal.Artifact.ID != output.Artifact.ID {
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
	for index := 0; index < 1000; index++ {
		if _, err = productService.CreateArtifact(ctx, product.ArtifactInput{ProjectID: project.ID,
			Name: fmt.Sprintf("unrelated-%04d.txt", index), Kind: "unrelated", MIMEType: "text/plain", Content: "noise"}); err != nil {
			t.Fatal(err)
		}
	}
	reviewed, err := New(tasks, productService, providerService).Review(ctx, authority, output.Proposal.ID, reviewerProfile.ID)
	if err != nil || reviewed.Proposal.State != taskengine.ProposalVerified || reviewed.Review.Verdict != "approve" || adapter.reviewCalls != 1 {
		t.Fatalf("reviewed output=%+v calls=%d err=%v", reviewed, adapter.reviewCalls, err)
	}
	finishedTask, err := tasks.Get(ctx, task.ID)
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
		Steps: []taskengine.StepSpec{{Key: "inspect", Title: "Inspect", Instructions: "inspect", EffectScope: []string{proposalEffect}}}})
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
