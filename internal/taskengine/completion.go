package taskengine

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// CompletionGate is a read projection of authoritative task records. A passing
// gate is evidence-backed; the projection never grants execution authority.
type CompletionGate struct {
	ID           string   `json:"id"`
	Label        string   `json:"label"`
	Status       string   `json:"status"`
	Reason       string   `json:"reason,omitempty"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
	ValidationID string   `json:"validation_id,omitempty"`
}

type CompletionReport struct {
	TaskID                 string           `json:"task_id"`
	TaskRevision           int              `json:"task_revision"`
	CanComplete            bool             `json:"can_complete"`
	Completed              bool             `json:"completed"`
	SubjectHash            string           `json:"subject_hash"`
	AwaitingUserAcceptance bool             `json:"awaiting_user_acceptance"`
	Gates                  []CompletionGate `json:"gates"`
}

type completionReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (s *Service) EvaluateCompletion(ctx context.Context, taskID string, expectedRevision int) (CompletionReport, error) {
	task, err := s.Get(ctx, taskID)
	if err != nil {
		return CompletionReport{}, err
	}
	if task.Revision != expectedRevision {
		return CompletionReport{}, ErrStaleRevision
	}
	return completionReport(ctx, s.store.DB, task)
}

func completionReport(ctx context.Context, reader completionReader, task Task) (CompletionReport, error) {
	report := CompletionReport{TaskID: task.ID, TaskRevision: task.Revision, CanComplete: task.State == StateVerifying,
		Completed: task.State == StateCompleted, Gates: []CompletionGate{}}
	add := func(gate CompletionGate) {
		if gate.Status != ValidationPass {
			report.CanComplete = false
		}
		report.Gates = append(report.Gates, gate)
	}
	stateGate := CompletionGate{ID: "state", Label: "Task state", Status: ValidationPass}
	if task.State != StateVerifying && task.State != StateCompleted {
		stateGate.Status, stateGate.Reason = ValidationBlocked, "Task must be verifying before completion"
	}
	add(stateGate)
	planGate := CompletionGate{ID: "plan", Label: "Current plan", Status: ValidationPass}
	if task.ActivePlanRevision == 0 || task.Plan.Revision != task.ActivePlanRevision ||
		task.Plan.RequirementRevision != task.ActiveRequirementRevision || len(task.Plan.Steps) == 0 {
		planGate.Status, planGate.Reason = ValidationBlocked, "A current plan with steps is required"
	}
	add(planGate)
	if task.Requirement.Contract.RequirePlanReview {
		reviewGate := CompletionGate{ID: "plan_review", Label: "Plan review", Status: ValidationPass}
		if task.Plan.ReviewStatus != "approved" {
			reviewGate.Status, reviewGate.Reason = ValidationBlocked, "The current plan is awaiting review"
		}
		add(reviewGate)
	}

	var effects int
	if err := reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_effect_intents WHERE task_id=? AND state IN ('planned','dispatched','uncertain')`, task.ID).Scan(&effects); err != nil {
		return CompletionReport{}, err
	}
	effectGate := CompletionGate{ID: "effects", Label: "Pending or uncertain actions", Status: ValidationPass}
	if effects > 0 {
		effectGate.Status, effectGate.Reason = ValidationBlocked, fmt.Sprintf("%d action(s) need a final receipt or reconciliation", effects)
	}
	add(effectGate)
	var desktopPending int
	if err := reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM desktop_actions da WHERE da.task_id=? AND
		(da.state IN ('prepared','dispatched') OR (da.state='uncertain' AND NOT EXISTS
		(SELECT 1 FROM desktop_action_reconciliations dr WHERE dr.action_id=da.id)))`, task.ID).Scan(&desktopPending); err != nil {
		return CompletionReport{}, err
	}
	desktopGate := CompletionGate{ID: "desktop_effects", Label: "Windows actions", Status: ValidationPass, EvidenceRefs: []string{}}
	if desktopPending > 0 {
		desktopGate.Status = ValidationBlocked
		desktopGate.Reason = fmt.Sprintf("%d Windows action(s) need cancellation or manual reconciliation", desktopPending)
	}
	rows, err := reader.QueryContext(ctx, `SELECT id FROM desktop_actions WHERE task_id=? AND state='completed' ORDER BY created_at DESC LIMIT 20`, task.ID)
	if err != nil {
		return CompletionReport{}, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return CompletionReport{}, err
		}
		desktopGate.EvidenceRefs = append(desktopGate.EvidenceRefs, "desktop_action:"+id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return CompletionReport{}, err
	}
	rows, err = reader.QueryContext(ctx, `SELECT dr.action_id,dr.screenshot_artifact_id FROM desktop_action_reconciliations dr
		JOIN desktop_actions da ON da.id=dr.action_id WHERE da.task_id=? AND dr.resolution='observed_applied'
		ORDER BY dr.created_at DESC LIMIT 20`, task.ID)
	if err != nil {
		return CompletionReport{}, err
	}
	for rows.Next() {
		var id, screenshotID string
		if err := rows.Scan(&id, &screenshotID); err != nil {
			rows.Close()
			return CompletionReport{}, err
		}
		desktopGate.EvidenceRefs = append(desktopGate.EvidenceRefs, "desktop_reconciliation:"+id, "artifact:"+screenshotID)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return CompletionReport{}, err
	}
	add(desktopGate)
	var browserPending int
	if err := reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM browser_action_receipts a WHERE a.task_id=? AND
		(a.state IN ('prepared','dispatched') OR (a.state='uncertain' AND NOT EXISTS
		(SELECT 1 FROM browser_action_reconciliations r WHERE r.action_id=a.id)))`, task.ID).Scan(&browserPending); err != nil {
		return CompletionReport{}, err
	}
	browserGate := CompletionGate{ID: "browser_effects", Label: "Browser actions", Status: ValidationPass, EvidenceRefs: []string{}}
	if browserPending > 0 {
		browserGate.Status = ValidationBlocked
		browserGate.Reason = fmt.Sprintf("%d browser action(s) need cancellation or manual reconciliation", browserPending)
	}
	rows, err = reader.QueryContext(ctx, `SELECT id,screenshot_after_id FROM browser_action_receipts WHERE task_id=? AND state='completed' ORDER BY created_at DESC LIMIT 20`, task.ID)
	if err != nil {
		return CompletionReport{}, err
	}
	for rows.Next() {
		var id, screenshot string
		if err := rows.Scan(&id, &screenshot); err != nil {
			rows.Close()
			return CompletionReport{}, err
		}
		browserGate.EvidenceRefs = append(browserGate.EvidenceRefs, "browser_action:"+id)
		if screenshot != "" {
			browserGate.EvidenceRefs = append(browserGate.EvidenceRefs, "artifact:"+screenshot)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return CompletionReport{}, err
	}
	rows, err = reader.QueryContext(ctx, `SELECT r.action_id,r.screenshot_artifact_id FROM browser_action_reconciliations r
		JOIN browser_action_receipts a ON a.id=r.action_id WHERE a.task_id=? AND r.resolution='observed_applied'
		ORDER BY r.created_at DESC LIMIT 20`, task.ID)
	if err != nil {
		return CompletionReport{}, err
	}
	for rows.Next() {
		var id, screenshot string
		if err := rows.Scan(&id, &screenshot); err != nil {
			rows.Close()
			return CompletionReport{}, err
		}
		browserGate.EvidenceRefs = append(browserGate.EvidenceRefs, "browser_reconciliation:"+id, "artifact:"+screenshot)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return CompletionReport{}, err
	}
	add(browserGate)
	var facebookPage, tiktokAccount, briefUpdated string
	contentErr := reader.QueryRowContext(ctx, `SELECT facebook_page,tiktok_account,updated_at FROM task_content_briefs WHERE task_id=?`, task.ID).Scan(&facebookPage, &tiktokAccount, &briefUpdated)
	if contentErr != nil && !errors.Is(contentErr, sql.ErrNoRows) {
		return CompletionReport{}, contentErr
	}
	if contentErr == nil {
		gate := CompletionGate{ID: "content_delivery", Label: "Content delivery", Status: ValidationPass, EvidenceRefs: []string{}}
		for _, target := range []struct{ platform, destination string }{{"facebook", facebookPage}, {"tiktok", tiktokAccount}} {
			if target.destination == "" {
				continue
			}
			var postID string
			err := reader.QueryRowContext(ctx, `SELECT sp.id FROM social_posts sp JOIN task_content_drafts d ON d.id=sp.draft_id
				WHERE sp.task_id=? AND sp.state='published' AND sp.remote_id<>'' AND sp.remote_status<>''
				AND d.platform=? AND d.destination=? AND sp.subject_hash IN (
					SELECT subject_hash FROM task_content_approvals WHERE draft_id=d.id AND task_id=sp.task_id)
				AND sp.created_at>=d.updated_at AND sp.created_at>=?
				AND EXISTS (SELECT 1 FROM task_content_claims c WHERE c.task_id=sp.task_id)
				AND NOT EXISTS (SELECT 1 FROM task_content_claims c JOIN task_research_candidates src ON src.id=c.source_id
					LEFT JOIN task_research_candidates src2 ON src2.id=c.corroborating_source_id
					WHERE c.task_id=sp.task_id AND (c.review_status<>'supported' OR src.selected<>1 OR src.source_digest<>c.source_digest
					OR (c.consequential=1 AND (src2.id IS NULL OR src2.selected<>1 OR src2.source_digest<>c.corroborating_source_digest))
					OR c.reviewed_at>sp.created_at OR c.created_at>sp.created_at))
				ORDER BY sp.created_at DESC LIMIT 1`, task.ID, target.platform, target.destination, briefUpdated).Scan(&postID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return CompletionReport{}, err
			}
			if err != nil {
				gate.Status = ValidationBlocked
				gate.Reason = "ยังไม่มีหลักฐานจากแพลตฟอร์มว่าเผยแพร่ฉบับปัจจุบันไปยัง " + target.destination
				break
			}
			gate.EvidenceRefs = append(gate.EvidenceRefs, "social_post:"+postID)
		}
		add(gate)
	}

	if task.ActivePlanRevision > 0 {
		var proposals int
		if err := reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_code_proposals p JOIN task_steps s ON s.id=p.step_id
			WHERE p.task_id=? AND s.plan_revision=? AND (
				p.state NOT IN ('verified','rejected','post_review_rejected','apply_failed','verification_failed') OR
				(p.state='verified' AND NOT EXISTS (
					SELECT 1 FROM task_code_reviews r WHERE r.proposal_id=p.id AND r.verdict='approved'
					AND r.reviewer <> ('provider:' || p.provider_id)
				))
			)`, task.ID, task.ActivePlanRevision).Scan(&proposals); err != nil {
			return CompletionReport{}, err
		}
		proposalGate := CompletionGate{ID: "review", Label: "Change review", Status: ValidationPass}
		if proposals > 0 {
			proposalGate.Status, proposalGate.Reason = ValidationBlocked, fmt.Sprintf("%d proposal(s) still require verification or independent review", proposals)
		}
		add(proposalGate)
	}

	for _, step := range task.Plan.Steps {
		gate := CompletionGate{ID: "step:" + step.ID, Label: step.Title, Status: ValidationPass}
		if step.State != StepCompleted {
			gate.Status, gate.Reason = ValidationBlocked, fmt.Sprintf("Step is %s; skipped steps need an explicit applicability decision", step.State)
		}
		add(gate)
		checkedStep := step
		if checkedStep.State == StepCompleted && checkedStep.Revision > 0 {
			// Completing a step increments its revision after its checks pass.
			checkedStep.Revision--
		}
		for _, check := range step.Checks {
			validation, err := latestCompletionValidation(ctx, reader, task.ID, step.ID, "", check, StepSubjectRevision(checkedStep))
			if err != nil {
				return CompletionReport{}, err
			}
			validation.ID, validation.Label = "check:"+step.ID+":"+check, check
			add(validation)
		}
	}
	for _, criterion := range task.Requirement.Criteria {
		validation, err := latestCompletionValidation(ctx, reader, task.ID, "", criterion.ID, "", RequirementSubjectRevision(task))
		if err != nil {
			return CompletionReport{}, err
		}
		validation.ID, validation.Label = "criterion:"+criterion.ID, criterion.Description
		add(validation)
	}
	subjectHash, err := completionSubjectHash(ctx, reader, task, report.Gates)
	if err != nil {
		return CompletionReport{}, err
	}
	report.SubjectHash = subjectHash
	if task.Requirement.Contract.RequireUserAcceptance {
		gate := CompletionGate{ID: "user_acceptance", Label: "User acceptance", Status: ValidationBlocked}
		if !report.CanComplete && !report.Completed {
			gate.Reason = "Finish current checks before accepting the result"
		} else {
			var acceptedID string
			err := reader.QueryRowContext(ctx, `SELECT id FROM task_user_acceptances WHERE task_id=? AND requirement_revision=? AND plan_revision=? AND subject_hash=? ORDER BY created_at DESC LIMIT 1`,
				task.ID, task.ActiveRequirementRevision, task.ActivePlanRevision, subjectHash).Scan(&acceptedID)
			if errors.Is(err, sql.ErrNoRows) {
				gate.Reason = "User acceptance is required for the current evidence"
				report.AwaitingUserAcceptance = true
			} else if err != nil {
				return CompletionReport{}, err
			} else {
				gate.Status, gate.EvidenceRefs = ValidationPass, []string{"acceptance:" + acceptedID}
			}
		}
		add(gate)
	}
	return report, nil
}

func completionSubjectHash(ctx context.Context, reader completionReader, task Task, gates []CompletionGate) (string, error) {
	type record struct {
		ID, State, UpdatedAt, Detail string
	}
	read := func(query string, args ...any) ([]record, error) {
		rows, err := reader.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		items := []record{}
		for rows.Next() {
			var item record
			if err := rows.Scan(&item.ID, &item.State, &item.UpdatedAt, &item.Detail); err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, rows.Err()
	}
	effects, err := read(`SELECT operation_id,state,updated_at,receipt_json FROM task_effect_intents WHERE task_id=? ORDER BY operation_id`, task.ID)
	if err != nil {
		return "", err
	}
	proposals, err := read(`SELECT p.id,p.state,p.updated_at,p.result_hash FROM task_code_proposals p JOIN task_steps s ON s.id=p.step_id WHERE p.task_id=? AND s.plan_revision=? ORDER BY p.id`, task.ID, task.ActivePlanRevision)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(struct {
		TaskID              string
		RequirementRevision int
		PlanRevision        int
		Gates               []CompletionGate
		Effects             []record
		Proposals           []record
	}{task.ID, task.ActiveRequirementRevision, task.ActivePlanRevision, gates, effects, proposals})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return fmt.Sprintf("%x", sum[:]), nil
}

func latestCompletionValidation(ctx context.Context, reader completionReader, taskID, stepID, requirementID, checkID, subjectRevision string) (CompletionGate, error) {
	gate := CompletionGate{Status: "not_run", Reason: "No validation for the current subject revision"}
	var status, evidenceJSON string
	query := `SELECT id,status,evidence_refs_json FROM task_validations WHERE task_id=? AND subject_revision=?`
	args := []any{taskID, subjectRevision}
	if stepID != "" {
		query += ` AND step_id=? AND check_id=?`
		args = append(args, stepID, checkID)
	} else {
		query += ` AND requirement_id=?`
		args = append(args, requirementID)
	}
	query += ` ORDER BY rowid DESC LIMIT 1`
	if err := reader.QueryRowContext(ctx, query, args...).Scan(&gate.ValidationID, &status, &evidenceJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			var previousID string
			oldQuery := `SELECT id FROM task_validations WHERE task_id=? AND subject_revision<>?`
			oldArgs := []any{taskID, subjectRevision}
			if stepID != "" {
				oldQuery += ` AND step_id=? AND check_id=?`
				oldArgs = append(oldArgs, stepID, checkID)
			} else {
				oldQuery += ` AND requirement_id=?`
				oldArgs = append(oldArgs, requirementID)
			}
			if oldErr := reader.QueryRowContext(ctx, oldQuery+` ORDER BY rowid DESC LIMIT 1`, oldArgs...).Scan(&previousID); oldErr == nil {
				gate.Status, gate.Reason = "stale", "Only a previous revision has validation; check the current goal and plan again"
			} else if !errors.Is(oldErr, sql.ErrNoRows) {
				return CompletionGate{}, oldErr
			}
			return gate, nil
		}
		return CompletionGate{}, err
	}
	gate.Status = status
	gate.Reason = ""
	if err := json.Unmarshal([]byte(evidenceJSON), &gate.EvidenceRefs); err != nil {
		return CompletionGate{}, fmt.Errorf("decode validation evidence: %w", err)
	}
	if status == ValidationPass && len(gate.EvidenceRefs) == 0 {
		gate.Status, gate.Reason = ValidationBlocked, "Passing validation has no evidence reference"
	} else if status != ValidationPass {
		gate.Reason = "Latest validation is " + status
	}
	return gate, nil
}
