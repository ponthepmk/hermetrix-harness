package taskengine

import (
	"context"
	"testing"
	"time"
)

func TestCompletionReportUsesLatestCurrentEvidence(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	initial, err := service.EvaluateCompletion(ctx, task.ID, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if initial.CanComplete || len(initial.Gates) == 0 {
		t.Fatalf("unverified task report = %+v", initial)
	}
	for index := range task.Plan.Steps {
		step := task.Plan.Steps[index]
		task, err = service.StartStep(ctx, task.ID, step.Key, task.Revision, step.Revision)
		if err != nil {
			t.Fatal(err)
		}
		step = task.Plan.Steps[index]
		for _, check := range step.Checks {
			if _, err = service.RecordValidation(ctx, Validation{TaskID: task.ID, StepID: step.ID, CheckID: check,
				SubjectRevision: StepSubjectRevision(step), Status: ValidationPass, EvidenceRefs: []string{"receipt:" + check}}); err != nil {
				t.Fatal(err)
			}
		}
		task, err = service.CompleteStep(ctx, task.ID, step.Key, task.Revision, step.Revision)
		if err != nil {
			t.Fatal(err)
		}
	}
	criterion := Validation{TaskID: task.ID, RequirementID: "AC-1", CheckID: "acceptance",
		SubjectRevision: RequirementSubjectRevision(task), Status: ValidationPass, EvidenceRefs: []string{"receipt:accepted"}}
	if _, err = service.RecordValidation(ctx, criterion); err != nil {
		t.Fatal(err)
	}
	ready, err := service.EvaluateCompletion(ctx, task.ID, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if !ready.CanComplete {
		t.Fatalf("completed checks did not open gate: %+v", ready)
	}
	criterion.Status = ValidationFail
	criterion.EvidenceRefs = []string{"receipt:regression"}
	if _, err = service.RecordValidation(ctx, criterion); err != nil {
		t.Fatal(err)
	}
	blocked, err := service.EvaluateCompletion(ctx, task.ID, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.CanComplete {
		t.Fatalf("later failed validation did not close gate: %+v", blocked)
	}
	if _, err = service.CompleteTask(ctx, task.ID, task.Revision); err == nil {
		t.Fatal("task completed after its latest acceptance validation failed")
	}
	criterion.Status = ValidationPass
	criterion.EvidenceRefs = []string{"receipt:rerun"}
	if _, err = service.RecordValidation(ctx, criterion); err != nil {
		t.Fatal(err)
	}
	if _, err = service.CompleteTask(ctx, task.ID, task.Revision); err != nil {
		t.Fatalf("task did not complete after a current passing rerun: %v", err)
	}
}

func TestCompletionDistinguishesOldEvidenceFromNeverRun(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	step := task.Plan.Steps[0]
	old := Validation{TaskID: task.ID, StepID: step.ID, CheckID: step.Checks[0], SubjectRevision: "previous-step-revision",
		Status: ValidationPass, EvidenceRefs: []string{"artifact:old"}}
	if _, err := service.RecordValidation(ctx, old); err != nil {
		t.Fatal(err)
	}
	report, err := service.EvaluateCompletion(ctx, task.ID, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	for _, gate := range report.Gates {
		if gate.ID == "check:"+step.ID+":"+step.Checks[0] {
			if gate.Status != "stale" || report.CanComplete {
				t.Fatalf("old check evidence=%+v report=%+v", gate, report)
			}
			return
		}
	}
	t.Fatal("check gate was omitted")
}

func TestBrowserActionRequiresFinalReceiptOrObservedReconciliation(t *testing.T) {
	service, dataStore := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	owner, err := dataStore.OwnerPrincipalID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := dataStore.DB.ExecContext(ctx, `INSERT INTO browser_tabs(id,url,state,created_at,updated_at) VALUES('tab-1','https://example.com','ready',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := dataStore.DB.ExecContext(ctx, `INSERT INTO browser_action_receipts(id,owner_principal_id,task_id,tab_id,action,element_ref,target_json,text_hash,
		observation_hash,subject_hash,state,created_at,expires_at) VALUES('action-1',?,?, 'tab-1','click',1,'{}','hash','before','subject','dispatched',?,?)`,
		owner, task.ID, now, now); err != nil {
		t.Fatal(err)
	}
	gate := func() CompletionGate {
		report, err := service.EvaluateCompletion(ctx, task.ID, task.Revision)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range report.Gates {
			if item.ID == "browser_effects" {
				return item
			}
		}
		t.Fatal("browser completion gate missing")
		return CompletionGate{}
	}
	if item := gate(); item.Status != ValidationBlocked {
		t.Fatalf("dispatched browser effect=%+v", item)
	}
	if _, err := dataStore.DB.ExecContext(ctx, `UPDATE browser_action_receipts SET state='uncertain' WHERE id='action-1'`); err != nil {
		t.Fatal(err)
	}
	if item := gate(); item.Status != ValidationBlocked {
		t.Fatalf("uncertain browser effect=%+v", item)
	}
	if _, err := dataStore.DB.ExecContext(ctx, `INSERT INTO browser_action_reconciliations(action_id,owner_principal_id,verification_tab_id,resolution,observation_hash,
		screenshot_artifact_id,actor,note,created_at) VALUES('action-1',?,'tab-1','observed_applied','after','image-1','owner','verified result',?)`, owner, now); err != nil {
		t.Fatal(err)
	}
	if item := gate(); item.Status != ValidationPass || len(item.EvidenceRefs) != 2 {
		t.Fatalf("reconciled browser effect=%+v", item)
	}
}

func TestCompletionRejectsUnresolvedEffectAfterChecksPass(t *testing.T) {
	service, _ := testService(t)
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
		StepKey: task.Plan.Steps[0].Key, ExpectedTaskRevision: task.Revision,
		ExpectedStepRevision: task.Plan.Steps[0].Revision, InputHash: "sha256:completion"})
	if err != nil {
		t.Fatal(err)
	}
	effect, err := service.PlanEffect(ctx, RunAuthority{RunID: run.ID, LeaseToken: run.LeaseToken}, attempt.ID,
		"workspace.run", "go test ./...", "task-plan")
	if err != nil {
		t.Fatal(err)
	}
	task, err = service.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	for index := range task.Plan.Steps {
		step := task.Plan.Steps[index]
		if index > 0 {
			task, err = service.StartStep(ctx, task.ID, step.Key, task.Revision, step.Revision)
			if err != nil {
				t.Fatal(err)
			}
			step = task.Plan.Steps[index]
		}
		for _, check := range step.Checks {
			if _, err = service.RecordValidation(ctx, Validation{TaskID: task.ID, StepID: step.ID, CheckID: check,
				SubjectRevision: StepSubjectRevision(step), Status: ValidationPass, EvidenceRefs: []string{"receipt:" + check}}); err != nil {
				t.Fatal(err)
			}
		}
		task, err = service.CompleteStep(ctx, task.ID, step.Key, task.Revision, step.Revision)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = service.RecordValidation(ctx, Validation{TaskID: task.ID, RequirementID: "AC-1", CheckID: "acceptance",
		SubjectRevision: RequirementSubjectRevision(task), Status: ValidationPass, EvidenceRefs: []string{"receipt:accepted"}}); err != nil {
		t.Fatal(err)
	}
	report, err := service.EvaluateCompletion(ctx, task.ID, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if report.CanComplete {
		t.Fatalf("unresolved effect %s did not block completion: %+v", effect.OperationID, report)
	}
	if _, err = service.CompleteTask(ctx, task.ID, task.Revision); err == nil {
		t.Fatal("task completed with a planned effect")
	}
}

func TestDesktopActionReconciliationControlsTaskGate(t *testing.T) {
	service, dataStore := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	ownerID, err := dataStore.OwnerPrincipalID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now := formatTime(time.Now().UTC())
	_, err = dataStore.DB.ExecContext(ctx, `INSERT INTO desktop_actions(id,owner_principal_id,task_id,window_handle,process_id,action,
		element_ref,target_json,text_hash,observation_hash,subject_hash,state,created_at,expires_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,'prepared',?,?)`, "native-1", ownerID, task.ID, 101, 202, "invoke", 1, "{}", "hash", "obs", "subject", now, now)
	if err != nil {
		t.Fatal(err)
	}
	gateStatus := func() string {
		report, err := service.EvaluateCompletion(ctx, task.ID, task.Revision)
		if err != nil {
			t.Fatal(err)
		}
		for _, gate := range report.Gates {
			if gate.ID == "desktop_effects" {
				return gate.Status
			}
		}
		t.Fatal("desktop gate missing")
		return ""
	}
	if got := gateStatus(); got != ValidationBlocked {
		t.Fatalf("prepared action gate=%s", got)
	}
	if _, err := dataStore.DB.ExecContext(ctx, `UPDATE desktop_actions SET state='uncertain' WHERE id='native-1'`); err != nil {
		t.Fatal(err)
	}
	if got := gateStatus(); got != ValidationBlocked {
		t.Fatalf("uncertain action gate=%s", got)
	}
	_, err = dataStore.DB.ExecContext(ctx, `INSERT INTO desktop_action_reconciliations(action_id,owner_principal_id,resolution,
		observation_hash,screenshot_artifact_id,actor,note,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		"native-1", ownerID, "observed_applied", "fresh-obs", "screenshot-1", "tester", "observed the changed window", now)
	if err != nil {
		t.Fatal(err)
	}
	if got := gateStatus(); got != ValidationPass {
		t.Fatalf("reconciled action gate=%s", got)
	}
}

func TestUserAcceptanceBindsToLatestEvidence(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task, err := service.Create(ctx, CreateTaskInput{Title: "Acceptance", Objective: "Prove the outcome",
		OriginalRequest: "Prove the outcome", Actor: "owner", Criteria: []Criterion{{ID: "AC-1", Description: "Checked outcome"}},
		Contract: GoalContract{Scope: "Current project", Assumptions: []string{"Model is ready"},
			AffectedJourneys: []string{"J1"}, Verification: "Run smoke check", RequireUserAcceptance: true}})
	if err != nil {
		t.Fatal(err)
	}
	task, err = service.CreatePlan(ctx, CreatePlanInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		RequirementRevision: task.ActiveRequirementRevision, Reason: "Check outcome", Actor: "planner",
		Steps: []StepSpec{{Key: "verify", Title: "Verify", Instructions: "Run smoke", RequirementIDs: []string{"AC-1"}, Checks: []string{"smoke"}}}})
	if err != nil {
		t.Fatal(err)
	}
	step := task.Plan.Steps[0]
	task, err = service.StartStep(ctx, task.ID, step.Key, task.Revision, step.Revision)
	if err != nil {
		t.Fatal(err)
	}
	step = task.Plan.Steps[0]
	if _, err = service.RecordValidation(ctx, Validation{TaskID: task.ID, StepID: step.ID, CheckID: "smoke",
		SubjectRevision: StepSubjectRevision(step), Status: ValidationPass, EvidenceRefs: []string{"receipt:smoke"}}); err != nil {
		t.Fatal(err)
	}
	task, err = service.CompleteStep(ctx, task.ID, step.Key, task.Revision, step.Revision)
	if err != nil {
		t.Fatal(err)
	}
	criterion := Validation{TaskID: task.ID, RequirementID: "AC-1", CheckID: "acceptance",
		SubjectRevision: RequirementSubjectRevision(task), Status: ValidationPass, EvidenceRefs: []string{"receipt:outcome"}}
	if _, err = service.RecordValidation(ctx, criterion); err != nil {
		t.Fatal(err)
	}
	report, err := service.EvaluateCompletion(ctx, task.ID, task.Revision)
	if err != nil || !report.AwaitingUserAcceptance || report.CanComplete || report.SubjectHash == "" {
		t.Fatalf("expected exact user acceptance gate: report=%+v err=%v", report, err)
	}
	decision, err := service.DecideNextAction(ctx, task.ID, task.Revision)
	if err != nil || decision.Decision.ActionID != "request_user_acceptance" {
		t.Fatalf("next action did not point to user acceptance: decision=%+v err=%v", decision, err)
	}
	if _, err = service.CompleteTask(ctx, task.ID, task.Revision); err == nil {
		t.Fatal("task completed without user acceptance")
	}
	if _, err = service.RecordUserAcceptance(ctx, task.ID, task.Revision, "stale-hash", "owner"); err != ErrStaleRevision {
		t.Fatalf("stale acceptance = %v, want stale revision", err)
	}
	accepted, err := service.RecordUserAcceptance(ctx, task.ID, task.Revision, report.SubjectHash, "owner")
	if err != nil || !accepted.CanComplete {
		t.Fatalf("current acceptance did not open completion: report=%+v err=%v", accepted, err)
	}
	if _, err = service.RecordValidation(ctx, criterion); err != nil {
		t.Fatal(err)
	}
	stale, err := service.EvaluateCompletion(ctx, task.ID, task.Revision)
	if err != nil || stale.CanComplete || !stale.AwaitingUserAcceptance || stale.SubjectHash == report.SubjectHash {
		t.Fatalf("new evidence did not invalidate acceptance: report=%+v err=%v", stale, err)
	}
	if _, err = service.CompleteTask(ctx, task.ID, task.Revision); err == nil {
		t.Fatal("task completed with stale user acceptance")
	}
	if _, err = service.RecordUserAcceptance(ctx, task.ID, task.Revision, stale.SubjectHash, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.CompleteTask(ctx, task.ID, task.Revision); err != nil {
		t.Fatalf("task did not complete with current acceptance: %v", err)
	}
}
