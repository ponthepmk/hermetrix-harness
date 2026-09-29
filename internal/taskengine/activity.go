package taskengine

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// TaskActivity is a bounded projection of durable decisions and evidence.
// It contains references and statuses, not prompts, document bodies or secrets.
type TaskActivity struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	Label        string    `json:"label"`
	Status       string    `json:"status"`
	Subject      string    `json:"subject,omitempty"`
	Current      bool      `json:"current"`
	EvidenceRefs []string  `json:"evidence_refs"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s *Service) ListActivity(ctx context.Context, taskID string, limit int) ([]TaskActivity, error) {
	task, err := s.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	report, err := completionReport(ctx, s.store.DB, task)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 200 {
		limit = 100
	}
	query := `SELECT id,kind,label,status,subject,evidence_refs_json,created_at,step_id,requirement_id,plan_revision,task_revision
	FROM (
	 SELECT id,'validation' kind,CASE WHEN requirement_id<>'' THEN requirement_id ELSE check_id END label,
	   status,subject_revision subject,evidence_refs_json,created_at,step_id,requirement_id,0 plan_revision,0 task_revision
	 FROM task_validations WHERE task_id=?
	 UNION ALL
	 SELECT id,'failure',failure_kind,'confirmed',normalized_signature,evidence_refs_json,created_at,step_id,'',0,0
	 FROM task_step_failures WHERE task_id=?
	 UNION ALL
	 SELECT id,'goal_revision','Goal r' || revision,'recorded',id,'[]',created_at,'','',0,revision
	 FROM task_requirement_revisions WHERE task_id=?
	 UNION ALL
	 SELECT id,'checkpoint',next_action,'recorded','',evidence_refs_json,created_at,'','',plan_revision,task_revision
	 FROM task_checkpoints WHERE task_id=?
	 UNION ALL
	 SELECT id,'plan_review','plan review',verdict,subject_hash,'[]',created_at,'','',plan_revision,0
	 FROM task_plan_reviews WHERE task_id=?
	 UNION ALL
	 SELECT id,'user_acceptance','user acceptance','accepted',subject_hash,'[]',created_at,'',CAST(requirement_revision AS TEXT),plan_revision,0
	 FROM task_user_acceptances WHERE task_id=?
	 UNION ALL
	 SELECT e.id,'effect',e.action,e.state,e.operation_id,'[]',e.created_at,a.step_id,'',r.plan_revision,r.requirement_revision
	 FROM task_effect_intents e JOIN task_step_attempts a ON a.id=e.attempt_id
	 JOIN task_runs r ON r.id=a.run_id WHERE e.task_id=?
	 UNION ALL
	 SELECT a.id,'browser_effect',a.action,
	   CASE WHEN r.resolution<>'' THEN 'reconciled_' || r.resolution ELSE a.state END,
	   a.subject_hash,
	   CASE WHEN r.screenshot_artifact_id<>'' THEN json_array('artifact:' || r.screenshot_artifact_id)
	     WHEN a.screenshot_before_id<>'' AND a.screenshot_after_id<>'' THEN json_array('artifact:' || a.screenshot_before_id,'artifact:' || a.screenshot_after_id)
	     WHEN a.screenshot_after_id<>'' THEN json_array('artifact:' || a.screenshot_after_id)
	     WHEN a.screenshot_before_id<>'' THEN json_array('artifact:' || a.screenshot_before_id) ELSE '[]' END,
	   a.created_at,'','',0,0
	 FROM browser_action_receipts a LEFT JOIN browser_action_reconciliations r ON r.action_id=a.id
	 WHERE a.task_id=? AND a.owner_principal_id=?
	 UNION ALL
	 SELECT a.id,'desktop_effect',a.action,
	   CASE WHEN r.resolution<>'' THEN 'reconciled_' || r.resolution ELSE a.state END,
	   a.subject_hash,
	   CASE WHEN r.screenshot_artifact_id<>'' THEN json_array('artifact:' || r.screenshot_artifact_id) ELSE '[]' END,
	   a.created_at,'','',0,0
	 FROM desktop_actions a LEFT JOIN desktop_action_reconciliations r ON r.action_id=a.id
	 WHERE a.task_id=? AND a.owner_principal_id=?
	 UNION ALL
	 SELECT j.id,'comfy_effect','Comfy workflow',j.state,j.operation_id,
	   (SELECT json_group_array('artifact:' || value) FROM json_each(j.result_artifact_ids_json)),
	   j.created_at,'','',0,0
	 FROM task_comfy_jobs j WHERE j.task_id=? AND j.owner_principal_id=?
	 UNION ALL
	 SELECT id,'social_effect',route,state,intent_hash,'[]',created_at,'','',0,0
	 FROM social_posts WHERE task_id=? AND owner_principal_id=?
	) ORDER BY created_at DESC,id DESC LIMIT ?`
	rows, err := s.store.DB.QueryContext(ctx, query, taskID, taskID, taskID, taskID, taskID, taskID, taskID,
		taskID, task.OwnerPrincipalID, taskID, task.OwnerPrincipalID, taskID, task.OwnerPrincipalID,
		taskID, task.OwnerPrincipalID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]TaskActivity, 0)
	criterionIDs := map[string]bool{}
	for _, criterion := range task.Requirement.Criteria {
		criterionIDs[criterion.ID] = true
	}
	steps := map[string]Step{}
	for _, step := range task.Plan.Steps {
		steps[step.ID] = step
	}
	for rows.Next() {
		var item TaskActivity
		var evidenceJSON, created, stepID, requirementID string
		var planRevision, taskRevision int
		if err := rows.Scan(&item.ID, &item.Kind, &item.Label, &item.Status, &item.Subject, &evidenceJSON,
			&created, &stepID, &requirementID, &planRevision, &taskRevision); err != nil {
			return nil, err
		}
		item.CreatedAt, err = parseTime(created)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(evidenceJSON), &item.EvidenceRefs); err != nil {
			return nil, fmt.Errorf("decode task activity evidence: %w", err)
		}
		if item.EvidenceRefs == nil {
			item.EvidenceRefs = []string{}
		}
		switch item.Kind {
		case "goal_revision":
			item.Current = taskRevision == task.ActiveRequirementRevision
		case "validation":
			if stepID != "" {
				if step, ok := steps[stepID]; ok {
					if step.State == StepCompleted && step.Revision > 0 {
						step.Revision--
					}
					item.Current = item.Subject == StepSubjectRevision(step)
				}
			} else {
				item.Current = criterionIDs[requirementID] && item.Subject == RequirementSubjectRevision(task)
			}
		case "checkpoint":
			item.Current = taskRevision == task.Revision && planRevision == task.ActivePlanRevision
		case "failure":
			_, item.Current = steps[stepID]
		case "plan_review":
			item.Current = planRevision == task.ActivePlanRevision && item.Subject == task.Plan.SubjectHash
		case "user_acceptance":
			reqRevision, _ := strconv.Atoi(requirementID)
			if reqRevision == task.ActiveRequirementRevision && planRevision == task.ActivePlanRevision {
				item.Current = item.Subject == report.SubjectHash
			}
		case "effect":
			_, exists := steps[stepID]
			item.Current = exists && planRevision == task.ActivePlanRevision && taskRevision == task.ActiveRequirementRevision
		case "browser_effect", "desktop_effect", "comfy_effect", "social_effect":
			item.Current = !item.CreatedAt.Before(task.Requirement.CreatedAt)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
