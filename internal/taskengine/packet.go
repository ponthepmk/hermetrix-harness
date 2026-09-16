package taskengine

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Keep the authoritative envelope below the worker boundary's 128 KiB input
// ceiling. This also leaves most of a 96k context window for selected source,
// tool schemas, reasoning and output instead of letting orchestration metadata
// consume the model's entire usable context.
const maxStepPacketBytes = 128 << 10

// StepPacket is the authoritative, bounded handoff to a planner or worker.
// It contains revisions and evidence references, not a warm transcript. A
// worker can therefore resume on another process/device without guessing
// which user constraint or plan revision was current.
type StepPacket struct {
	TaskID              string              `json:"task_id"`
	TaskRevision        int                 `json:"task_revision"`
	ProjectID           string              `json:"project_id,omitempty"`
	Objective           string              `json:"objective"`
	OriginalRequest     string              `json:"original_request"`
	Requirement         RequirementRevision `json:"requirement"`
	PlanRevision        int                 `json:"plan_revision"`
	Step                Step                `json:"step"`
	CompletedSteps      []string            `json:"completed_steps"`
	Checkpoint          *Checkpoint         `json:"checkpoint,omitempty"`
	ValidationEvidence  []Validation        `json:"validation_evidence"`
	UnresolvedEffects   []EffectIntent      `json:"unresolved_effects"`
	BuiltAt             time.Time           `json:"built_at"`
	CanonicalPacketHash string              `json:"canonical_packet_hash"`
}

func (s *Service) BuildNextStepPacket(ctx context.Context, taskID string) (StepPacket, error) {
	task, err := s.Get(ctx, taskID)
	if err != nil {
		return StepPacket{}, err
	}
	if task.ActivePlanRevision == 0 {
		return StepPacket{}, fmt.Errorf("task has no active plan")
	}
	step, completed, err := nextRunnableStep(task.Plan.Steps)
	if err != nil {
		return StepPacket{}, err
	}
	packet := StepPacket{TaskID: task.ID, TaskRevision: task.Revision, ProjectID: task.ProjectID,
		Objective: task.Objective, OriginalRequest: task.OriginalRequest, Requirement: task.Requirement,
		PlanRevision: task.ActivePlanRevision, Step: step, CompletedSteps: completed, BuiltAt: time.Now().UTC()}
	if checkpoint, checkpointErr := s.latestCheckpoint(ctx, task.ID); checkpointErr == nil {
		packet.Checkpoint = &checkpoint
	} else if !errors.Is(checkpointErr, sql.ErrNoRows) {
		return StepPacket{}, checkpointErr
	}
	packet.ValidationEvidence, err = s.validationEvidence(ctx, task.ID)
	if err != nil {
		return StepPacket{}, err
	}
	packet.UnresolvedEffects, err = s.unresolvedEffects(ctx, task.ID)
	if err != nil {
		return StepPacket{}, err
	}
	packet.CanonicalPacketHash, err = canonicalPacketHash(packet)
	if err != nil {
		return StepPacket{}, err
	}
	return packet, nil
}

func ValidateStepPacket(packet StepPacket) error {
	want, err := canonicalPacketHash(packet)
	if err != nil {
		return err
	}
	if packet.CanonicalPacketHash == "" || packet.CanonicalPacketHash != want {
		return fmt.Errorf("step packet hash does not match its authoritative content")
	}
	return nil
}

func canonicalPacketHash(packet StepPacket) (string, error) {
	packet.BuiltAt = time.Time{}
	packet.CanonicalPacketHash = ""
	encoded, err := json.Marshal(packet)
	if err != nil {
		return "", err
	}
	if len(encoded) > maxStepPacketBytes {
		return "", fmt.Errorf("authoritative step packet is %d bytes, exceeds %d; split or revise the task instead of truncating constraints", len(encoded), maxStepPacketBytes)
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func nextRunnableStep(steps []Step) (Step, []string, error) {
	states := map[string]string{}
	completed := []string{}
	for _, step := range steps {
		states[step.Key] = step.State
		if step.State == StepCompleted || step.State == StepSkipped {
			completed = append(completed, step.Key)
		}
	}
	for _, step := range steps {
		if step.State != StepPending && step.State != StepBlocked && step.State != StepFailed {
			continue
		}
		ready := true
		for _, dependency := range step.Dependencies {
			if states[dependency] != StepCompleted && states[dependency] != StepSkipped {
				ready = false
				break
			}
		}
		if ready {
			return step, completed, nil
		}
	}
	for _, step := range steps {
		if step.State == StepRunning {
			return Step{}, completed, fmt.Errorf("task already has running step %s", step.Key)
		}
	}
	return Step{}, completed, fmt.Errorf("task has no runnable step")
}

func (s *Service) latestCheckpoint(ctx context.Context, taskID string) (Checkpoint, error) {
	var item Checkpoint
	var completed, pending, evidence, unresolved, prerequisites, created string
	err := s.store.DB.QueryRowContext(ctx, `SELECT id,task_id,plan_revision,task_revision,completed_steps_json,pending_steps_json,
		evidence_refs_json,unresolved_effects_json,next_action,resume_prerequisites_json,reason,created_at
		FROM task_checkpoints WHERE task_id=? ORDER BY created_at DESC LIMIT 1`, taskID).
		Scan(&item.ID, &item.TaskID, &item.PlanRevision, &item.TaskRevision, &completed, &pending, &evidence,
			&unresolved, &item.NextAction, &prerequisites, &item.Reason, &created)
	if err != nil {
		return item, err
	}
	_ = json.Unmarshal([]byte(completed), &item.CompletedSteps)
	_ = json.Unmarshal([]byte(pending), &item.PendingSteps)
	_ = json.Unmarshal([]byte(evidence), &item.EvidenceRefs)
	_ = json.Unmarshal([]byte(unresolved), &item.UnresolvedEffects)
	_ = json.Unmarshal([]byte(prerequisites), &item.ResumePrerequisites)
	item.CreatedAt, _ = parseTime(created)
	return item, nil
}

func (s *Service) validationEvidence(ctx context.Context, taskID string) ([]Validation, error) {
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id,task_id,step_id,requirement_id,check_id,subject_revision,status,
		expected,actual,evidence_refs_json,severity,confidence,created_at FROM task_validations WHERE task_id=? ORDER BY created_at`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Validation{}
	for rows.Next() {
		var item Validation
		var evidence, created string
		if err = rows.Scan(&item.ID, &item.TaskID, &item.StepID, &item.RequirementID, &item.CheckID,
			&item.SubjectRevision, &item.Status, &item.Expected, &item.Actual, &evidence, &item.Severity, &item.Confidence, &created); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(evidence), &item.EvidenceRefs)
		item.CreatedAt, _ = parseTime(created)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) unresolvedEffects(ctx context.Context, taskID string) ([]EffectIntent, error) {
	rows, err := s.store.DB.QueryContext(ctx, `SELECT operation_id FROM task_effect_intents WHERE task_id=? AND state IN ('planned','dispatched','uncertain') ORDER BY created_at`, taskID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	items := make([]EffectIntent, 0, len(ids))
	for _, id := range ids {
		item, getErr := s.getEffect(ctx, id)
		if getErr != nil {
			return nil, getErr
		}
		items = append(items, item)
	}
	return items, nil
}
