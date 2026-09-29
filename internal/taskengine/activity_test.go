package taskengine

import (
	"context"
	"testing"
	"time"
)

func TestTaskActivityRetainsOldEvidenceButMarksOnlyCurrentRevision(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	for _, entry := range []Validation{
		{TaskID: task.ID, RequirementID: "AC-1", CheckID: "acceptance", SubjectRevision: "old-requirement", Status: ValidationPass, EvidenceRefs: []string{"artifact:old"}},
		{TaskID: task.ID, RequirementID: "AC-1", CheckID: "acceptance", SubjectRevision: RequirementSubjectRevision(task), Status: ValidationPass, EvidenceRefs: []string{"artifact:current"}},
	} {
		if _, err := service.RecordValidation(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}
	items, err := service.ListActivity(ctx, task.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	var current, stale int
	for _, item := range items {
		if item.Kind != "validation" || item.Label != "AC-1" {
			continue
		}
		if item.Current && len(item.EvidenceRefs) == 1 && item.EvidenceRefs[0] == "artifact:current" {
			current++
		}
		if !item.Current && len(item.EvidenceRefs) == 1 && item.EvidenceRefs[0] == "artifact:old" {
			stale++
		}
	}
	if current != 1 || stale != 1 {
		t.Fatalf("activity current=%d old=%d items=%+v", current, stale, items)
	}
}

func TestTaskActivityIncludesCurrentEffectStatusWithoutReceiptBody(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	run, err := service.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		Owner: "runner", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	task, err = service.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := service.BeginStepAttempt(ctx, BeginAttemptInput{RunID: run.ID, LeaseToken: run.LeaseToken,
		StepKey: task.Plan.Steps[0].Key, ExpectedTaskRevision: task.Revision,
		ExpectedStepRevision: task.Plan.Steps[0].Revision, InputHash: "sha256:activity"})
	if err != nil {
		t.Fatal(err)
	}
	effect, err := service.PlanEffect(ctx, RunAuthority{RunID: run.ID, LeaseToken: run.LeaseToken}, attempt.ID,
		"workspace.run", "go test ./...", "task-plan")
	if err != nil {
		t.Fatal(err)
	}
	items, err := service.ListActivity(ctx, task.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == effect.ID {
			if item.Kind != "effect" || item.Status != string(EffectPlanned) || !item.Current ||
				item.Subject != effect.OperationID || len(item.EvidenceRefs) != 0 {
				t.Fatalf("effect activity = %+v", item)
			}
			return
		}
	}
	t.Fatalf("effect missing from activity: %+v", items)
}

func TestTaskActivityLinksConfirmedFailureAndRollbackReceipt(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	run, err := service.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		Owner: "runner", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	task, err = service.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	step := task.Plan.Steps[0]
	attempt, err := service.BeginStepAttempt(ctx, BeginAttemptInput{RunID: run.ID, LeaseToken: run.LeaseToken,
		StepKey: step.Key, ExpectedTaskRevision: task.Revision, ExpectedStepRevision: step.Revision,
		InputHash: "activity-failure"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.FailAttempt(ctx, attempt.ID, "mandatory check failed"); err != nil {
		t.Fatal(err)
	}
	decision, err := service.RecordConfirmedFailure(ctx, ConfirmedFailureInput{AttemptID: attempt.ID,
		FailureKind: "test", Message: "mandatory check failed",
		EvidenceRefs: []string{"job:failed-check", "rollback_artifact:rollback-receipt"}})
	if err != nil {
		t.Fatal(err)
	}
	items, err := service.ListActivity(ctx, task.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == decision.FailureID {
			if item.Kind != "failure" || !item.Current || item.Status != "confirmed" ||
				len(item.EvidenceRefs) != 2 ||
				!(item.EvidenceRefs[0] == "rollback_artifact:rollback-receipt" || item.EvidenceRefs[1] == "rollback_artifact:rollback-receipt") {
				t.Fatalf("failure activity=%+v", item)
			}
			return
		}
	}
	t.Fatalf("confirmed failure missing from activity: %+v", items)
}

func TestTaskActivityShowsExternalEffectsWithoutExposingDraftOrActionText(t *testing.T) {
	service, database := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	at := task.Requirement.CreatedAt.Add(time.Second).UTC().Format(time.RFC3339Nano)
	owner := task.OwnerPrincipalID
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO browser_tabs(id,url,state,created_at,updated_at) VALUES(?,'https://example.test','ready',?,?)`, []any{"activity-tab", at, at}},
		{`INSERT INTO browser_action_receipts(id,owner_principal_id,task_id,tab_id,action,element_ref,target_json,text_hash,observation_hash,subject_hash,screenshot_before_id,state,created_at,expires_at)
			VALUES(?,?,?,'activity-tab','click',1,'{}','digest','observation','browser-subject','browser-before','completed',?,?)`, []any{"browser-effect", owner, task.ID, at, at}},
		{`INSERT INTO desktop_actions(id,owner_principal_id,task_id,window_handle,process_id,action,element_ref,target_json,text_hash,observation_hash,subject_hash,actor,state,created_at,expires_at)
			VALUES(?,?,?,1,1,'invoke',1,'{}','digest','observation','desktop-subject','owner','uncertain',?,?)`, []any{"desktop-effect", owner, task.ID, at, at}},
		{`INSERT INTO desktop_action_reconciliations(action_id,owner_principal_id,resolution,observation_hash,screenshot_artifact_id,actor,note,created_at)
			VALUES('desktop-effect',?,'observed_applied','observation-2','desktop-after','owner','verified',?)`, []any{owner, at}},
		{`INSERT INTO task_comfy_jobs(id,task_id,project_id,owner_principal_id,workflow_path,workflow_hash,staged_path,operation_id,state,result_artifact_ids_json,created_at,updated_at)
			VALUES(?,?,?,?,'private-workflow.json','digest','private-stage.json','comfy-operation','completed','["comfy-video"]',?,?)`, []any{"comfy-effect", task.ID, task.ProjectID, owner, at, at}},
		{`INSERT INTO social_connections(id,owner_principal_id,platform,account_id,display_name,capability,status,checked_at,created_at)
			VALUES('activity-connection',?,'facebook','page-id','Private Page','feed','ready',?,?)`, []any{owner, at, at}},
		{`INSERT INTO task_content_drafts(id,task_id,owner_principal_id,platform,destination,body,created_at,updated_at)
			VALUES('activity-draft',?,?,'facebook','page-id','Private draft text',?,?)`, []any{task.ID, owner, at, at}},
		{`INSERT INTO social_posts(id,task_id,draft_id,connection_id,owner_principal_id,subject_hash,intent_hash,route,state,created_at,updated_at)
			VALUES('social-effect',?,'activity-draft','activity-connection',?,'subject','social-intent','facebook_feed','approved',?,?)`, []any{task.ID, owner, at, at}},
	}
	for _, statement := range statements {
		if _, err := database.DB.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	items, err := service.ListActivity(ctx, task.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		kind, status, evidence string
	}{
		"browser-effect": {"browser_effect", "completed", "artifact:browser-before"},
		"desktop-effect": {"desktop_effect", "reconciled_observed_applied", "artifact:desktop-after"},
		"comfy-effect":   {"comfy_effect", "completed", "artifact:comfy-video"},
		"social-effect":  {"social_effect", "approved", ""},
	}
	for _, item := range items {
		expected, relevant := want[item.ID]
		if !relevant {
			continue
		}
		if item.Kind != expected.kind || item.Status != expected.status || !item.Current ||
			item.Subject == "Private draft text" || item.Subject == "private-workflow.json" {
			t.Fatalf("external effect activity = %+v", item)
		}
		if expected.evidence == "" && len(item.EvidenceRefs) != 0 ||
			expected.evidence != "" && (len(item.EvidenceRefs) != 1 || item.EvidenceRefs[0] != expected.evidence) {
			t.Fatalf("external effect evidence = %+v", item)
		}
		delete(want, item.ID)
	}
	if len(want) != 0 {
		t.Fatalf("missing external effects: %v; items=%+v", want, items)
	}
}
