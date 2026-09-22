package taskengine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const RuleDecisionRevision = "rule-decision-v1"

const (
	maxCompactItems = 64
	maxCompactGoal  = 4096
)

// CompactState is a bounded, evidence-referenced view of one durable task.
// It is derived from authoritative task records and is never a second source
// of truth for requirements, plans, validations, or effects.
type CompactState struct {
	TaskID              string            `json:"task_id"`
	TaskRevision        int               `json:"task_revision"`
	RequirementRevision int               `json:"requirement_revision"`
	PlanRevision        int               `json:"plan_revision"`
	Goal                string            `json:"goal"`
	TaskState           string            `json:"task_state"`
	CurrentStep         *CompactStep      `json:"current_step,omitempty"`
	CompletedSteps      []string          `json:"completed_steps"`
	FailedChecks        []string          `json:"failed_checks"`
	EvidenceRefs        []string          `json:"evidence_refs"`
	UnresolvedEffects   []string          `json:"unresolved_effects"`
	Criteria            map[string]string `json:"criteria"`
	LatestCheckpoint    *Checkpoint       `json:"latest_checkpoint,omitempty"`
	ConsecutiveFailures int               `json:"consecutive_failures"`
	CompletionEligible  bool              `json:"completion_eligible"`
}

type CompactStep struct {
	ID          string   `json:"id"`
	Key         string   `json:"key"`
	Revision    int      `json:"revision"`
	State       string   `json:"state"`
	Checks      []string `json:"checks"`
	EffectScope []string `json:"effect_scope"`
	LastError   string   `json:"last_error,omitempty"`
}

type CandidateAction struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	Target         string `json:"target,omitempty"`
	Risk           string `json:"risk"`
	Cost           string `json:"cost"`
	RequiresPolicy bool   `json:"requires_policy"`
	Reason         string `json:"reason"`
}

type DecisionResult struct {
	ActionID       string   `json:"action_id"`
	Backend        string   `json:"backend"`
	LatencyMS      int64    `json:"latency_ms"`
	FallbackUsed   bool     `json:"fallback_used"`
	Confidence     *float64 `json:"confidence,omitempty"`
	DecisionMargin *float64 `json:"decision_margin,omitempty"`
	Reason         string   `json:"reason"`
}

type NextActionDecision struct {
	State      CompactState      `json:"state"`
	Candidates []CandidateAction `json:"candidates"`
	Decision   DecisionResult    `json:"decision"`
}

// DecisionEngine selects one candidate but has no authority to execute it.
type DecisionEngine interface {
	Decide(context.Context, CompactState, []CandidateAction) (DecisionResult, error)
}

// RuleDecision is the deterministic baseline used before a model-backed
// decision engine is admitted by evaluation.
type RuleDecision struct{}

var _ DecisionEngine = RuleDecision{}

func (RuleDecision) Decide(ctx context.Context, state CompactState, candidates []CandidateAction) (DecisionResult, error) {
	if err := ctx.Err(); err != nil {
		return DecisionResult{}, err
	}
	return ruleDecision(state, candidates)
}

// DecideNextAction produces a read-only recommendation. Callers must still
// submit the selected action through its existing policy and revision gate.
func (s *Service) DecideNextAction(ctx context.Context, taskID string, expectedRevision int) (NextActionDecision, error) {
	started := time.Now()
	state, err := s.BuildCompactState(ctx, taskID, expectedRevision)
	if err != nil {
		return NextActionDecision{}, err
	}
	candidates := candidateActions(state)
	decision, err := (RuleDecision{}).Decide(ctx, state, candidates)
	if err != nil {
		return NextActionDecision{}, err
	}
	decision.LatencyMS = time.Since(started).Milliseconds()
	return NextActionDecision{State: state, Candidates: candidates, Decision: decision}, nil
}

func (s *Service) BuildCompactState(ctx context.Context, taskID string, expectedRevision int) (CompactState, error) {
	task, err := s.Get(ctx, taskID)
	if err != nil {
		return CompactState{}, err
	}
	if task.Revision != expectedRevision {
		return CompactState{}, ErrStaleRevision
	}
	state := CompactState{TaskID: task.ID, TaskRevision: task.Revision,
		RequirementRevision: task.ActiveRequirementRevision, PlanRevision: task.ActivePlanRevision,
		Goal: boundedDecisionText(task.Objective, maxCompactGoal), TaskState: task.State, Criteria: map[string]string{}}
	if len(task.Requirement.Criteria) > maxCompactItems {
		return CompactState{}, fmt.Errorf("task has %d acceptance criteria; compact decision state supports at most %d", len(task.Requirement.Criteria), maxCompactItems)
	}
	for _, criterion := range task.Requirement.Criteria {
		state.Criteria[criterion.ID] = ValidationUnknown
	}
	for _, step := range task.Plan.Steps {
		if step.State == StepCompleted || step.State == StepSkipped {
			state.CompletedSteps = append(state.CompletedSteps, step.Key)
		}
	}
	if step, _, nextErr := nextRunnableStep(task.Plan.Steps); nextErr == nil {
		state.CurrentStep = compactStep(step)
	} else {
		for _, step := range task.Plan.Steps {
			if step.State == StepRunning {
				state.CurrentStep = compactStep(step)
				break
			}
		}
	}
	validations, err := s.validationEvidence(ctx, task.ID)
	if err != nil {
		return CompactState{}, err
	}
	latestCheckStatus := map[string]Validation{}
	for _, item := range validations {
		state.EvidenceRefs = append(state.EvidenceRefs, item.EvidenceRefs...)
		latestCheckStatus[item.StepID+"\x00"+item.CheckID+"\x00"+item.SubjectRevision] = item
		if _, active := state.Criteria[item.RequirementID]; active && item.SubjectRevision == RequirementSubjectRevision(task) {
			state.Criteria[item.RequirementID] = item.Status
		}
	}
	for _, item := range latestCheckStatus {
		if item.Status == ValidationFail || item.Status == ValidationBlocked {
			state.FailedChecks = append(state.FailedChecks, item.CheckID)
		}
	}
	effects, err := s.unresolvedEffects(ctx, task.ID)
	if err != nil {
		return CompactState{}, err
	}
	for _, effect := range effects {
		state.UnresolvedEffects = append(state.UnresolvedEffects, effect.OperationID)
	}
	if checkpoint, checkpointErr := s.latestCheckpoint(ctx, task.ID); checkpointErr == nil {
		checkpoint.CompletedSteps = boundedDecisionStrings(checkpoint.CompletedSteps, maxCompactItems)
		checkpoint.PendingSteps = boundedDecisionStrings(checkpoint.PendingSteps, maxCompactItems)
		checkpoint.EvidenceRefs = boundedDecisionStrings(checkpoint.EvidenceRefs, maxCompactItems)
		checkpoint.UnresolvedEffects = boundedDecisionStrings(checkpoint.UnresolvedEffects, maxCompactItems)
		checkpoint.ResumePrerequisites = boundedDecisionStrings(checkpoint.ResumePrerequisites, maxCompactItems)
		checkpoint.NextAction = boundedDecisionText(checkpoint.NextAction, 1024)
		checkpoint.Reason = boundedDecisionText(checkpoint.Reason, 1024)
		state.LatestCheckpoint = &checkpoint
		state.EvidenceRefs = append(state.EvidenceRefs, checkpoint.EvidenceRefs...)
	} else if !errors.Is(checkpointErr, sql.ErrNoRows) {
		return CompactState{}, checkpointErr
	}
	state.ConsecutiveFailures, err = s.consecutiveFailureCount(ctx, task)
	if err != nil {
		return CompactState{}, err
	}
	state.CompletedSteps = boundedDecisionStrings(uniqueSorted(state.CompletedSteps), maxCompactItems)
	state.FailedChecks = boundedDecisionStrings(uniqueSorted(state.FailedChecks), maxCompactItems)
	state.EvidenceRefs = boundedDecisionStrings(uniqueSorted(state.EvidenceRefs), maxCompactItems)
	state.UnresolvedEffects = boundedDecisionStrings(uniqueSorted(state.UnresolvedEffects), maxCompactItems)
	state.CompletionEligible = completionEligible(task, state)
	return state, nil
}

func compactStep(step Step) *CompactStep {
	return &CompactStep{ID: step.ID, Key: step.Key, Revision: step.Revision, State: step.State,
		Checks: append([]string(nil), step.Checks...), EffectScope: append([]string(nil), step.EffectScope...), LastError: step.LastError}
}

func (s *Service) consecutiveFailureCount(ctx context.Context, task Task) (int, error) {
	if len(task.Plan.Steps) == 0 {
		return 0, nil
	}
	var stepID string
	for _, step := range task.Plan.Steps {
		if step.State == StepFailed || step.State == StepBlocked || step.State == StepRunning || step.State == StepPending {
			stepID = step.ID
			break
		}
	}
	if stepID == "" {
		return 0, nil
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT normalized_signature FROM task_step_failures
		WHERE step_id=? AND created_at > COALESCE((SELECT MAX(created_at) FROM task_step_escalations WHERE step_id=?),'')
		ORDER BY created_at DESC,id DESC LIMIT 4`, stepID, stepID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	count, signature := 0, ""
	for rows.Next() {
		var current string
		if err := rows.Scan(&current); err != nil {
			return 0, err
		}
		if signature == "" {
			signature = current
		}
		if current != signature {
			break
		}
		count++
	}
	return count, rows.Err()
}

func completionEligible(task Task, state CompactState) bool {
	if task.State != StateVerifying || len(state.UnresolvedEffects) > 0 || len(task.Plan.Steps) == 0 {
		return false
	}
	for _, step := range task.Plan.Steps {
		if step.State != StepCompleted && step.State != StepSkipped {
			return false
		}
	}
	for _, status := range state.Criteria {
		if status != ValidationPass {
			return false
		}
	}
	return true
}

func candidateActions(state CompactState) []CandidateAction {
	if len(state.UnresolvedEffects) > 0 {
		return []CandidateAction{{ID: "reconcile_effects", Type: "reconcile_effects", Risk: "write",
			Cost: "medium", RequiresPolicy: true, Reason: "an existing effect has no final receipt"}}
	}
	if state.CompletionEligible {
		return []CandidateAction{{ID: "finish", Type: "finish", Risk: "transition",
			Cost: "low", RequiresPolicy: true, Reason: "all active criteria and steps have passing evidence"}}
	}
	if state.PlanRevision == 0 || state.ConsecutiveFailures >= 2 || state.TaskState == StatePaused && state.CurrentStep == nil {
		return []CandidateAction{{ID: "retrieve_memory", Type: "retrieve_memory", Target: state.TaskID, Risk: "read", Cost: "low", Reason: "look for reviewed prior knowledge before replanning"},
			{ID: "ask_planner", Type: "ask_planner", Target: state.TaskID, Risk: "model", Cost: "medium", RequiresPolicy: true, Reason: "the task has no safe runnable step or repeated the same failure"}}
	}
	if state.CurrentStep == nil {
		return []CandidateAction{{ID: "ask_planner", Type: "ask_planner", Target: state.TaskID, Risk: "model", Cost: "medium", RequiresPolicy: true, Reason: "no runnable step is available"}}
	}
	items := []CandidateAction{{ID: "inspect_file", Type: "inspect_file", Target: state.CurrentStep.ID, Risk: "read", Cost: "low", Reason: "gather source evidence for the current step"},
		{ID: "search_repository", Type: "search_repository", Target: state.CurrentStep.ID, Risk: "read", Cost: "low", Reason: "locate related symbols and working sibling paths"},
		{ID: "retrieve_memory", Type: "retrieve_memory", Target: state.TaskID, Risk: "read", Cost: "low", Reason: "reuse reviewed project knowledge when relevant"}}
	if len(state.CurrentStep.Checks) > 0 {
		items = append(items, CandidateAction{ID: "run_test", Type: "run_test", Target: state.CurrentStep.ID,
			Risk: "execute", Cost: "medium", RequiresPolicy: true, Reason: "the current step declares executable checks"})
	}
	return items
}

func ruleDecision(state CompactState, candidates []CandidateAction) (DecisionResult, error) {
	if len(candidates) == 0 {
		return DecisionResult{}, fmt.Errorf("decision has no candidate actions")
	}
	preferred := "inspect_file"
	reason := "inspect the current step before choosing an effect"
	switch {
	case len(state.UnresolvedEffects) > 0:
		preferred, reason = "reconcile_effects", "unresolved effects block every new action"
	case state.CompletionEligible:
		preferred, reason = "finish", "the deterministic completion gate is satisfied"
	case state.ConsecutiveFailures >= 2:
		preferred, reason = "retrieve_memory", "the same failure repeated; retrieve reviewed knowledge before replanning"
	case state.PlanRevision == 0 || state.CurrentStep == nil:
		preferred, reason = "ask_planner", "the task has no safe runnable step"
	case len(state.FailedChecks) > 0 && hasCandidate(candidates, "run_test"):
		preferred, reason = "run_test", "a declared check has current failure evidence"
	}
	if !hasCandidate(candidates, preferred) {
		preferred = candidates[0].ID
		reason = candidates[0].Reason
	}
	return DecisionResult{ActionID: preferred, Backend: RuleDecisionRevision, Reason: reason}, nil
}

func hasCandidate(items []CandidateAction, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	items := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			items = append(items, value)
		}
	}
	sort.Strings(items)
	return items
}

func boundedDecisionStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	return append([]string(nil), values[:limit]...)
}

func boundedDecisionText(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
