package taskengine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
)

// RecordUserAcceptance binds a human decision to the exact current evidence.
// A later validation, effect or proposal change changes the subject hash and
// invalidates this decision without deleting its historical receipt.
func (s *Service) RecordUserAcceptance(ctx context.Context, taskID string, expectedRevision int, subjectHash, actor string) (CompletionReport, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" || subjectHash == "" {
		return CompletionReport{}, fmt.Errorf("user acceptance requires actor and subject hash")
	}
	task, err := s.Get(ctx, taskID)
	if err != nil {
		return CompletionReport{}, err
	}
	if task.Revision != expectedRevision {
		return CompletionReport{}, ErrStaleRevision
	}
	if task.State != StateVerifying || !task.Requirement.Contract.RequireUserAcceptance {
		return CompletionReport{}, fmt.Errorf("task is not awaiting user acceptance")
	}
	report, err := s.EvaluateCompletion(ctx, taskID, expectedRevision)
	if err != nil {
		return CompletionReport{}, err
	}
	if report.SubjectHash != subjectHash {
		return CompletionReport{}, ErrStaleRevision
	}
	if !report.AwaitingUserAcceptance {
		if report.CanComplete {
			return report, nil
		}
		return CompletionReport{}, fmt.Errorf("current checks must pass before user acceptance")
	}
	_, err = s.store.DB.ExecContext(ctx, `INSERT OR IGNORE INTO task_user_acceptances
		(id,task_id,requirement_revision,plan_revision,subject_hash,actor,created_at) VALUES(?,?,?,?,?,?,?)`,
		identity.New("acceptance"), taskID, task.ActiveRequirementRevision, task.ActivePlanRevision, subjectHash, actor, formatTime(time.Now().UTC()))
	if err != nil {
		return CompletionReport{}, err
	}
	return s.EvaluateCompletion(ctx, taskID, expectedRevision)
}
