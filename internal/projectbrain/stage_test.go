package projectbrain

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/taskengine"
)

type stageFixture struct {
	data       *store.Store
	stage      StageService
	input      StageInput
	principal  string
	projectID  string
	artifactID string
	validation taskengine.Validation
	ctx        context.Context
}

func newStageFixture(t *testing.T) stageFixture {
	t.Helper()
	ctx := context.Background()
	data, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	owner, err := data.LocalPrincipalID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ctx = identity.WithPrincipal(ctx, owner)
	projectID := "local-project-1"
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := data.DB.ExecContext(ctx, `INSERT INTO projects
		(id,name,root_path,state,created_at,updated_at,owner_principal_id,visibility,export_policy,sharing_revision)
		VALUES(?,?,?,'active',?,?,?,'project_shared','explicit_selection',2)`,
		projectID, "Fixture", t.TempDir(), now, now, owner); err != nil {
		t.Fatal(err)
	}
	tasks := taskengine.NewService(data)
	task, err := tasks.Create(ctx, taskengine.CreateTaskInput{ProjectID: projectID,
		Title: "Repair a migration", Objective: "Make database reopen work",
		OriginalRequest: "Reopening after migration failed", Actor: owner,
		Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "reopen succeeds"}}})
	if err != nil {
		t.Fatal(err)
	}
	task, err = tasks.CreatePlan(ctx, taskengine.CreatePlanInput{TaskID: task.ID,
		ExpectedTaskRevision: task.Revision, RequirementRevision: task.ActiveRequirementRevision,
		Reason: "fix regression", Actor: owner,
		Steps: []taskengine.StepSpec{{Key: "fix", Title: "Fix", Instructions: "repair and test",
			RequirementIDs: []string{"AC-1"}, Checks: []string{"migration-reopen"}}}})
	if err != nil {
		t.Fatal(err)
	}
	step := task.Plan.Steps[0]
	task, err = tasks.StartStep(ctx, task.ID, step.Key, task.Revision, step.Revision)
	if err != nil {
		t.Fatal(err)
	}
	step = task.Plan.Steps[0]
	body := []byte("PASS: migration reopen and schema integrity")
	checksum, err := data.Blobs.Put(body)
	if err != nil {
		t.Fatal(err)
	}
	artifactID := "artifact-test-1"
	created := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := data.DB.ExecContext(ctx, `INSERT INTO artifacts
		(id,project_id,name,kind,mime_type,blob_ref,byte_size,checksum,metadata_json,created_at,
		owner_principal_id,visibility,export_policy,sharing_revision)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,'project_shared','explicit_selection',3)`,
		artifactID, projectID, "reopen.txt", "code_verification_bundle", "text/plain",
		checksum, len(body), checksum, "{}", created, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.RecordValidation(ctx, taskengine.Validation{TaskID: task.ID,
		StepID: step.ID, CheckID: "migration-reopen", SubjectRevision: taskengine.StepSubjectRevision(step),
		Status: taskengine.ValidationPass, EvidenceRefs: []string{"artifact:" + artifactID}}); err != nil {
		t.Fatal(err)
	}
	task, err = tasks.CompleteStep(ctx, task.ID, step.Key, task.Revision, step.Revision)
	if err != nil {
		t.Fatal(err)
	}
	validation, err := tasks.RecordValidation(ctx, taskengine.Validation{TaskID: task.ID,
		RequirementID: "AC-1", CheckID: "migration-reopen",
		SubjectRevision: taskengine.RequirementSubjectRevision(task), Status: taskengine.ValidationPass,
		EvidenceRefs: []string{"artifact:" + artifactID}})
	if err != nil {
		t.Fatal(err)
	}
	task, err = tasks.CompleteTask(ctx, task.ID, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := data.DB.ExecContext(ctx, `UPDATE durable_tasks SET visibility='project_shared',
		export_policy='explicit_selection',sharing_revision=2 WHERE id=?`, task.ID); err != nil {
		t.Fatal(err)
	}
	piProject := "hermetrix-harness"
	solution := "Wrap migration in a transaction, reopen SQLite, and verify schema."
	contentDigest, err := CandidateExportContentDigest(CommittedTaskFacts{ProjectID: piProject,
		HarnessTaskID: task.ID, TaskRevision: task.Revision, Title: task.Title,
		Problem: task.OriginalRequest, Solution: solution})
	if err != nil {
		t.Fatal(err)
	}
	approval := CandidateExportApproval{Actor: owner, ProjectID: piProject,
		HarnessTaskID: task.ID, TaskRevision: task.Revision,
		ValidationID: validation.ID, CheckID: validation.CheckID,
		SubjectRevision: validation.SubjectRevision, VerificationKind: "test", ContentDigest: contentDigest,
		ProjectSharingRevision: 2, TaskSharingRevision: 2,
		EvidenceDigests:   map[string]string{artifactID: "sha256:" + checksum},
		EvidenceRevisions: map[string]int{artifactID: 3},
		ApprovedAt:        task.UpdatedAt.Add(time.Second)}
	outbox := &CandidateOutbox{DB: data.DB}
	stage := StageService{Store: data, Outbox: outbox, LocalProjectID: projectID,
		PiProjectID: piProject, OriginNodeID: "node-windows-1", AgentID: "hermetrix-windows"}
	return stageFixture{data: data, stage: stage, principal: owner, projectID: projectID,
		artifactID: artifactID, validation: validation, ctx: ctx,
		input: StageInput{TaskID: task.ID, ValidationID: validation.ID,
			SelectedArtifactIDs: []string{artifactID}, Solution: solution,
			VerificationKind: "test", Approval: approval}}
}

func (f stageFixture) queuedMessage(t *testing.T, id string) CandidateMessage {
	t.Helper()
	var projectID, taskID, digest, payload, approvalJSON string
	if err := f.data.DB.QueryRowContext(f.ctx, `SELECT project_id,task_id,payload_digest,payload_json,
		approval_json FROM project_brain_candidate_outbox WHERE candidate_id=?`, id).
		Scan(&projectID, &taskID, &digest, &payload, &approvalJSON); err != nil {
		t.Fatal(err)
	}
	var approval CandidateExportApproval
	if err := json.Unmarshal([]byte(approvalJSON), &approval); err != nil {
		t.Fatal(err)
	}
	return CandidateMessage{CandidateID: id, ProjectID: projectID, TaskID: taskID,
		PayloadDigest: digest, Payload: json.RawMessage(payload), Approval: approval}
}

func TestStageExplicitSelectedCompletedTaskAndReauthorizeBeforeSend(t *testing.T) {
	f := newStageFixture(t)
	queued, err := f.stage.Stage(f.ctx, f.input)
	if err != nil || queued.State != "pending" || queued.TaskID != f.input.TaskID {
		t.Fatalf("stage: %+v %v", queued, err)
	}
	message := f.queuedMessage(t, queued.CandidateID)
	if err := f.stage.AuthorizeQueued(context.Background(), message); err != nil {
		t.Fatalf("fresh owner selection was rejected before send: %v", err)
	}
	var candidate KnowledgeCandidate
	if err := json.Unmarshal(message.Payload, &candidate); err != nil ||
		candidate.ProjectID != f.stage.PiProjectID || candidate.Verification.Outcome != "pass" ||
		len(candidate.Verification.EvidenceRefs) != 1 || candidate.Verification.EvidenceRefs[0].EvidenceID != f.artifactID ||
		candidate.TaskID != "" || candidate.RunID != "" {
		t.Fatalf("candidate wire facts are wrong: %+v %v", candidate, err)
	}
	repeated, err := f.stage.Stage(f.ctx, f.input)
	if err != nil || repeated.CandidateID != queued.CandidateID {
		t.Fatalf("same explicit selection was not idempotent: %+v %v", repeated, err)
	}
}

func TestStageRejectsPrivateUnselectedOrUnverifiedSource(t *testing.T) {
	cases := map[string]func(t *testing.T, f *stageFixture){
		"private task": func(t *testing.T, f *stageFixture) {
			_, err := f.data.DB.ExecContext(f.ctx, `UPDATE durable_tasks SET export_policy='deny' WHERE id=?`, f.input.TaskID)
			if err != nil {
				t.Fatal(err)
			}
		},
		"private project": func(t *testing.T, f *stageFixture) {
			_, err := f.data.DB.ExecContext(f.ctx, `UPDATE projects SET visibility='private' WHERE id=?`, f.projectID)
			if err != nil {
				t.Fatal(err)
			}
		},
		"private artifact": func(t *testing.T, f *stageFixture) {
			_, err := f.data.DB.ExecContext(f.ctx, `UPDATE artifacts SET export_policy='deny' WHERE id=?`, f.artifactID)
			if err != nil {
				t.Fatal(err)
			}
		},
		"uncited artifact": func(t *testing.T, f *stageFixture) {
			_, err := f.data.DB.ExecContext(f.ctx, `UPDATE task_validations SET evidence_refs_json='[]' WHERE id=?`, f.validation.ID)
			if err != nil {
				t.Fatal(err)
			}
		},
		"superseded pass": func(t *testing.T, f *stageFixture) {
			_, err := f.data.DB.ExecContext(f.ctx, `INSERT INTO task_validations
				(id,task_id,requirement_id,check_id,subject_revision,status,evidence_refs_json,created_at)
				VALUES(?,?,?,?,?,'fail','[]',?)`, "newer-fail", f.input.TaskID, "AC-1",
				f.validation.CheckID, f.validation.SubjectRevision, f.validation.CreatedAt.Add(time.Second).Format(time.RFC3339Nano))
			if err != nil {
				t.Fatal(err)
			}
		},
		"missing CAS": func(t *testing.T, f *stageFixture) {
			_, err := f.data.DB.ExecContext(f.ctx, `UPDATE artifacts SET blob_ref=? WHERE id=?`,
				"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", f.artifactID)
			if err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newStageFixture(t)
			mutate(t, &f)
			if queued, err := f.stage.Stage(f.ctx, f.input); err == nil {
				t.Fatalf("unsafe export was queued: %+v", queued)
			}
			var count int
			if err := f.data.DB.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM project_brain_candidate_outbox`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("failed staging wrote outbox row: count=%d err=%v", count, err)
			}
		})
	}
}

func TestStageBlocksRevokedQueuedApprovalEvenAfterReallow(t *testing.T) {
	f := newStageFixture(t)
	queued, err := f.stage.Stage(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	message := f.queuedMessage(t, queued.CandidateID)
	if _, err := f.data.DB.ExecContext(f.ctx, `UPDATE artifacts SET export_policy='deny',sharing_revision=sharing_revision+1 WHERE id=?`, f.artifactID); err != nil {
		t.Fatal(err)
	}
	if err := f.stage.AuthorizeQueued(context.Background(), message); !errors.Is(err, ErrExportRevoked) {
		t.Fatalf("revoked artifact still authorized: %v", err)
	}
	if _, err := f.data.DB.ExecContext(f.ctx, `UPDATE artifacts SET export_policy='explicit_selection',sharing_revision=sharing_revision+1 WHERE id=?`, f.artifactID); err != nil {
		t.Fatal(err)
	}
	if err := f.stage.AuthorizeQueued(context.Background(), message); !errors.Is(err, ErrExportRevoked) {
		t.Fatalf("old approval resurrected after policy reallow: %v", err)
	}
}

func TestStageRequiresTrustedOwnerPrincipal(t *testing.T) {
	f := newStageFixture(t)
	if queued, err := f.stage.Stage(context.Background(), f.input); err == nil {
		t.Fatalf("unbound actor selected an export: %+v", queued)
	}
	var count int
	if err := f.data.DB.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM project_brain_candidate_outbox`).Scan(&count); err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unbound actor left %d queued candidates", count)
	}
}

func TestStageEligibilityAndPreviewExposeOnlySelectedOwnerMetadata(t *testing.T) {
	f := newStageFixture(t)
	eligible, err := f.stage.Eligibility(f.ctx, f.input.TaskID)
	if err != nil || !eligible.Eligible || len(eligible.Validations) != 1 ||
		eligible.Validations[0].ID != f.validation.ID || len(eligible.Validations[0].Artifacts) != 1 ||
		eligible.Validations[0].Artifacts[0].ID != f.artifactID ||
		!eligible.Validations[0].Artifacts[0].Eligible {
		t.Fatalf("owner selection metadata: %+v %v", eligible, err)
	}
	encoded, err := json.Marshal(eligible)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "PASS: migration reopen and schema integrity") {
		t.Fatal("eligibility leaked immutable evidence bytes")
	}
	preview, err := f.stage.Preview(f.ctx, f.input.TaskID, f.validation.ID,
		[]string{f.artifactID}, f.input.Solution, "test")
	if err != nil || preview.Candidate.Verification.Outcome != "pass" ||
		preview.Approval.ContentDigest == "" || preview.Approval.VerificationKind != "test" ||
		preview.Approval.EvidenceDigests[f.artifactID] == "" {
		t.Fatalf("preview did not bind the owner-selected facts: %+v %v", preview, err)
	}
	var count int
	if err := f.data.DB.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM project_brain_candidate_outbox`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("preview queued candidate: %d %v", count, err)
	}
	f.input.Approval = preview.Approval
	if _, err := f.stage.Stage(f.ctx, f.input); err != nil {
		t.Fatalf("unmodified preview approval must stage: %v", err)
	}
}

func TestStagePreviewRejectsDeniedAndChangedReview(t *testing.T) {
	f := newStageFixture(t)
	preview, err := f.stage.Preview(f.ctx, f.input.TaskID, f.validation.ID,
		[]string{f.artifactID}, f.input.Solution, "test")
	if err != nil {
		t.Fatal(err)
	}
	f.input.Approval = preview.Approval
	f.input.VerificationKind = "review"
	if _, err := f.stage.Stage(f.ctx, f.input); err == nil {
		t.Fatal("changed verification kind bypassed reviewed approval")
	}
	f.input.VerificationKind = "test"
	f.input.Solution = "A different unreviewed solution"
	if _, err := f.stage.Stage(f.ctx, f.input); err == nil {
		t.Fatal("changed solution bypassed reviewed approval")
	}
	if _, err := f.data.DB.ExecContext(f.ctx, `UPDATE artifacts SET export_policy='deny',
		sharing_revision=sharing_revision+1 WHERE id=?`, f.artifactID); err != nil {
		t.Fatal(err)
	}
	eligibility, err := f.stage.Eligibility(f.ctx, f.input.TaskID)
	if err != nil || eligibility.Eligible {
		t.Fatalf("revoked artifact still eligible: %+v %v", eligibility, err)
	}
	if _, err := f.stage.Preview(f.ctx, f.input.TaskID, f.validation.ID,
		[]string{f.artifactID}, f.input.Solution, "test"); err == nil {
		t.Fatal("revoked artifact allowed a preview")
	}
}
