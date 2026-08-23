package learning

import (
	"context"
	"errors"
	"testing"

	"hermetrix-harness/internal/runtime"
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
)

func setupLearning(t *testing.T, reviewer Reviewer) (*Service, *skills.Service, *runtime.InferenceGate) {
	t.Helper()
	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })
	skillService := skills.NewService(dataStore)
	gate := runtime.NewInferenceGate()
	return NewService(dataStore, skillService, gate, reviewer), skillService, gate
}

func TestReviewQueueIsIdempotentAndNoChangeIsValid(t *testing.T) {
	service, skillService, _ := setupLearning(t, StructuredReviewer{})
	ctx := context.Background()
	input := EnqueueInput{SessionID: "session-1", MilestoneID: "milestone-1", TriggerKind: "successful_milestone",
		Digest: Digest{GoalAndConstraints: "finish the task safely", Outcome: "success"}}
	first, duplicate, err := service.Enqueue(ctx, input)
	if err != nil || duplicate {
		t.Fatalf("enqueue: job=%+v duplicate=%v err=%v", first, duplicate, err)
	}
	second, duplicate, err := service.Enqueue(ctx, input)
	if err != nil || !duplicate || second.ID != first.ID {
		t.Fatalf("duplicate enqueue: job=%+v duplicate=%v err=%v", second, duplicate, err)
	}
	completed, err := service.RunNext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != StateCompleted || completed.Decision.Kind != "no_change" || completed.CandidateID != "" {
		t.Fatalf("completed = %+v", completed)
	}
	active, _ := skillService.ListSkills(ctx, false)
	if len(active) != 0 {
		t.Fatal("no-change review created an active skill")
	}
}

func TestBackgroundReviewCreatesCandidateOnly(t *testing.T) {
	service, skillService, _ := setupLearning(t, StructuredReviewer{})
	ctx := context.Background()
	markdown := "---\nname: learned-review\ndescription: \"Review a learned workflow\"\ntags: [learned]\ntools: []\n---\n\n# Procedure\n\n1. Read evidence.\n"
	job, _, err := service.Enqueue(ctx, EnqueueInput{SessionID: "session-2", JobID: "job-2", MilestoneID: "m2",
		TriggerKind: "explicit_learn", Digest: Digest{GoalAndConstraints: "reuse this review", Outcome: "success",
			SuggestedSkill: &SuggestedSkill{CanonicalName: "learned-review", ScopeKind: "user", Owner: "user",
				ChangeKind: "create", Reason: "explicit reusable procedure", Markdown: markdown}}})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := service.RunNext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if completed.ID != job.ID || completed.CandidateID == "" || completed.Decision.Kind != "create" {
		t.Fatalf("completed = %+v", completed)
	}
	candidate, err := skillService.GetCandidate(ctx, completed.CandidateID)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.CreatedBy != "background_reviewer" || candidate.SourceReviewID != job.ID || candidate.State != skills.CandidateNeedsReview {
		t.Fatalf("candidate provenance = %+v", candidate)
	}
	active, _ := skillService.ListSkills(ctx, false)
	if len(active) != 0 {
		t.Fatal("background review bypassed promotion")
	}
	promoted, err := skillService.PromoteCandidate(ctx, candidate.ID, "user", candidate.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if promoted.Origin != "agent_promoted" {
		t.Fatalf("promoted origin = %q", promoted.Origin)
	}
}

type blockingReviewer struct{ started chan struct{} }

func (b blockingReviewer) Revision() string { return "blocking-v1" }
func (b blockingReviewer) Review(ctx context.Context, _ Digest) (Decision, error) {
	close(b.started)
	<-ctx.Done()
	return Decision{}, ctx.Err()
}

func TestForegroundPreemptionRequeuesReview(t *testing.T) {
	reviewer := blockingReviewer{started: make(chan struct{})}
	service, _, gate := setupLearning(t, reviewer)
	ctx := context.Background()
	job, _, err := service.Enqueue(ctx, EnqueueInput{SessionID: "session-3", MilestoneID: "m3",
		TriggerKind: "batch", Digest: Digest{GoalAndConstraints: "background work"}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan Job, 1)
	go func() { result, _ := service.RunNext(ctx); done <- result }()
	<-reviewer.started
	if err := gate.RunForeground(ctx, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	result := <-done
	if result.ID != job.ID || result.State != StateQueued || result.Attempts != 1 {
		t.Fatalf("requeued job = %+v", result)
	}
	if result.Error != "preempted by foreground work" {
		t.Fatalf("requeue reason = %q", result.Error)
	}
}

func TestRunNextReportsEmptyQueue(t *testing.T) {
	service, _, _ := setupLearning(t, StructuredReviewer{})
	if _, err := service.RunNext(context.Background()); !errors.Is(err, ErrNoQueuedReview) {
		t.Fatalf("error = %v", err)
	}
}

func TestInterruptedReviewIsRecoveredAfterRestart(t *testing.T) {
	service, _, _ := setupLearning(t, StructuredReviewer{})
	ctx := context.Background()
	job, _, err := service.Enqueue(ctx, EnqueueInput{SessionID: "session-restart", MilestoneID: "m-restart",
		TriggerKind: "batch", Digest: Digest{GoalAndConstraints: "survive restart"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.DB.ExecContext(ctx, `UPDATE learning_reviews SET state=? WHERE id=?`, StateRunning, job.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := service.RecoverInterrupted(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d", recovered)
	}
	items, err := service.List(ctx, StateQueued)
	if err != nil || len(items) != 1 || items[0].Error != "requeued after process interruption" {
		t.Fatalf("recovered jobs=%+v err=%v", items, err)
	}
}

func TestLearningTriggerPolicyRequiresObservedEvidence(t *testing.T) {
	service, _, _ := setupLearning(t, StructuredReviewer{})
	ctx := context.Background()
	tests := []EnqueueInput{
		{SessionID: "s", MilestoneID: "success", TriggerKind: "successful_milestone", Digest: Digest{Outcome: "failure"}},
		{SessionID: "s", MilestoneID: "correction", TriggerKind: "repeated_correction", Digest: Digest{UserCorrections: []string{"once"}}},
		{SessionID: "s", MilestoneID: "skill", TriggerKind: "skill_failure", Digest: Digest{Outcome: "failure"}},
		{SessionID: "s", MilestoneID: "unknown", TriggerKind: "invented", Digest: Digest{}},
	}
	for _, input := range tests {
		if _, _, err := service.Enqueue(ctx, input); err == nil {
			t.Fatalf("trigger %s accepted without required evidence", input.TriggerKind)
		}
	}
	valid := EnqueueInput{SessionID: "s", MilestoneID: "valid", TriggerKind: "repeated_correction",
		Digest: Digest{UserCorrections: []string{"first", "second"}}}
	if _, _, err := service.Enqueue(ctx, valid); err != nil {
		t.Fatal(err)
	}
}

// stubReviewer proposes a fixed procedure so the runner path can be tested
// without a provider.
type stubReviewer struct{ decision Decision }

func (stubReviewer) Revision() string { return "stub-reviewer-v1" }
func (r stubReviewer) Review(_ context.Context, _ Digest) (Decision, error) {
	return r.decision, nil
}

// O-9: the runner used to read the *digest's* suggestion and ignore the
// reviewer's, so a reviewer that wanted to propose something new had nowhere to
// put it and every review from real work ended in no_change.
func TestReviewerProposalBecomesACandidateAndNothingMore(t *testing.T) {
	proposal := Decision{Kind: "create", Reason: "the same correction came up twice",
		SuggestedSkill: &SuggestedSkill{CanonicalName: "satang-rounding", ScopeKind: "user", Owner: "user",
			ChangeKind: "create", Reason: "observed in completed work",
			Markdown: "---\nname: satang-rounding\ndescription: \"Round Thai money half up in satang\"\ntags: []\ntools: []\n---\n\n# Procedure\n\n1. Keep amounts as integers.\n"}}
	service, skillService, _ := setupLearning(t, stubReviewer{decision: proposal})
	ctx := context.Background()
	if _, _, err := service.Enqueue(ctx, EnqueueInput{SessionID: "session-1", MilestoneID: "turn-1",
		TriggerKind: "repeated_correction", Digest: Digest{GoalAndConstraints: "fix the rounding",
			Outcome: "success", UserCorrections: []string{"event:a", "event:b"}}}); err != nil {
		t.Fatal(err)
	}
	job, err := service.RunNext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if job.CandidateID == "" {
		t.Fatalf("a reviewer proposal produced no candidate: %+v", job.Decision)
	}
	candidate, err := skillService.GetCandidate(ctx, job.CandidateID)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.CanonicalName != "satang-rounding" {
		t.Fatalf("candidate does not carry the proposal: %+v", candidate)
	}
	if candidate.Origin != "agent_candidate" {
		t.Fatalf("origin is %q; a proposal must not look like trusted knowledge", candidate.Origin)
	}
	if candidate.CreatedBy != "background_reviewer" {
		t.Fatalf("created_by is %q, want background_reviewer", candidate.CreatedBy)
	}
	// The whole point of the authority ladder: nothing became active.
	active, err := skillService.ListSkills(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("a reviewer proposal reached the active store: %+v", active)
	}
}

func TestReviewerDeclineCreatesNothing(t *testing.T) {
	service, skillService, _ := setupLearning(t, stubReviewer{
		decision: Decision{Kind: "no_change", Reason: "one-off work, no procedure"}})
	ctx := context.Background()
	if _, _, err := service.Enqueue(ctx, EnqueueInput{SessionID: "session-1", MilestoneID: "turn-1",
		TriggerKind: "successful_milestone", Digest: Digest{GoalAndConstraints: "read a file",
			Outcome: "success", ToolReceipts: []string{"event:a:workspace.read_file:succeeded"}}}); err != nil {
		t.Fatal(err)
	}
	job, err := service.RunNext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if job.CandidateID != "" {
		t.Fatalf("a decline still created candidate %s", job.CandidateID)
	}
	candidates, err := skillService.ListCandidates(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("a decline left %d candidates behind", len(candidates))
	}
}
