package agentplatform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/taskengine"
)

const initialObservationKey = "initial-read-only-decision-v1"

type assignmentProjection struct {
	AssignmentID       string
	AssignmentRevision int
	PlatformRunID      string
	HarnessTaskID      string
	BindingID          string
	BindingRevision    int
	ProjectID          string
	OwnerPrincipalID   string
}

type projectionSnapshot struct {
	Kind                string                       `json:"kind"`
	AssignmentID        string                       `json:"assignment_id"`
	AssignmentRevision  int                          `json:"assignment_revision"`
	PlatformRunID       string                       `json:"platform_run_id"`
	HarnessTaskID       string                       `json:"harness_task_id"`
	TaskRevision        int                          `json:"task_revision"`
	RequirementRevision int                          `json:"requirement_revision"`
	PlanRevision        int                          `json:"plan_revision"`
	State               taskengine.CompactState      `json:"compact_state"`
	Candidates          []taskengine.CandidateAction `json:"read_only_candidates"`
	Selection           taskengine.DecisionResult    `json:"recorded_selection"`
	ObservedAt          string                       `json:"observed_at"`
}

// ObserveInitial records one decision-only observation. It cannot execute the
// selected action because Service contains no executor or dispatch capability.
func (s *Service) ObserveInitial(ctx context.Context, assignmentID string) (Observation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, found, err := s.existingObservation(ctx, assignmentID, initialObservationKey); err != nil || found {
		return prior, err
	}
	record, err := s.loadProjection(ctx, assignmentID)
	if err != nil {
		return Observation{}, err
	}
	if _, err = s.resolvePinnedBinding(ctx, record.BindingID, record.BindingRevision); err != nil {
		return Observation{}, err
	}
	decision, err := s.tasks.DecideNextAction(identity.WithPrincipal(ctx, record.OwnerPrincipalID), record.HarnessTaskID, 1)
	if err != nil {
		return Observation{}, err
	}
	if decision.State.TaskState != taskengine.StateDraft || decision.State.TaskRevision != 1 ||
		decision.State.RequirementRevision != 1 || decision.State.PlanRevision != 0 || decision.State.CurrentStep != nil ||
		len(decision.State.CompletedSteps) != 0 || len(decision.State.FailedChecks) != 0 ||
		len(decision.State.UnresolvedEffects) != 0 || len(decision.State.EvidenceRefs) != 0 {
		return Observation{}, fmt.Errorf("%w: task is outside the fresh H1 projection profile", ErrUnsupported)
	}
	allowed := make([]taskengine.CandidateAction, 0, len(decision.Candidates))
	for _, candidate := range decision.Candidates {
		if candidate.Risk == "read" && !candidate.RequiresPolicy && allowedDecisionType(candidate.Type) {
			allowed = append(allowed, candidate)
		}
	}
	started := time.Now()
	selection, err := (taskengine.RuleDecision{}).Decide(ctx, decision.State, allowed)
	if err != nil {
		return Observation{}, err
	}
	selection.LatencyMS = time.Since(started).Milliseconds()
	if !containsCandidate(allowed, selection.ActionID) {
		return Observation{}, fmt.Errorf("%w: decision selected an action outside the supplied set", ErrValidation)
	}
	s.decisions.Add(1)

	now := formatContractTime(s.trust.now())
	snapshot := projectionSnapshot{Kind: "h1.read_only_observation", AssignmentID: record.AssignmentID,
		AssignmentRevision: record.AssignmentRevision, PlatformRunID: record.PlatformRunID,
		HarnessTaskID: record.HarnessTaskID, TaskRevision: decision.State.TaskRevision,
		RequirementRevision: decision.State.RequirementRevision, PlanRevision: decision.State.PlanRevision,
		State: decision.State, Candidates: allowed, Selection: selection, ObservedAt: now}
	snapshotRaw, err := json.Marshal(snapshot)
	if err != nil {
		return Observation{}, err
	}
	snapshotRaw, err = CanonicalizeJSON(snapshotRaw)
	if err != nil {
		return Observation{}, err
	}
	blobRef, err := s.store.Blobs.Put(snapshotRaw)
	if err != nil {
		return Observation{}, err
	}
	artifactID := identity.New("artifact")
	projectionRef := EvidenceRef{EvidenceID: artifactID, Type: "artifact", OriginNodeID: s.trust.NodeID,
		ContentDigest: "sha256:" + blobRef, CreatedAt: now,
		Provenance: map[string]any{"kind": "h1.read_only_observation", "task_revision": 1, "requirement_revision": 1}}

	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Observation{}, err
	}
	defer tx.Rollback()
	var sequence int64
	if err = tx.QueryRowContext(ctx, `SELECT next_sequence FROM agent_platform_streams WHERE platform_id=? AND platform_run_id=?`,
		s.trust.PlatformID, record.PlatformRunID).Scan(&sequence); err != nil {
		return Observation{}, err
	}
	update := RunUpdate{PlatformRunID: record.PlatformRunID, Sequence: sequence, ExecutionStatus: "accepted",
		Progress: ProgressFacts{Phase: "initializing", CompletedSteps: 0, TotalSteps: 0, AttemptCount: 0,
			FailedChecks: 0, BlockedReasons: []string{}}, ProjectionRef: projectionRef, CreatedAt: now}
	envelopeRaw, envelope, err := SignEnvelope("RunUpdate", RunUpdateRevision, s.trust.NodeID,
		fmt.Sprintf("%s:%d", record.PlatformRunID, sequence), update, s.trust.KeyID, s.trust.PrivateKey)
	if err != nil {
		return Observation{}, err
	}
	if err = validateSchema("run_update", envelope.Payload); err != nil {
		return Observation{}, fmt.Errorf("%w: generated run update: %v", ErrValidation, err)
	}
	metadata, _ := json.Marshal(map[string]any{"platform_run_id": record.PlatformRunID, "sequence": sequence,
		"projection_digest": projectionRef.ContentDigest})
	if _, err = tx.ExecContext(ctx, `INSERT INTO artifacts
		(id,project_id,name,kind,mime_type,blob_ref,byte_size,checksum,metadata_json,created_at,owner_principal_id,visibility,export_policy,sharing_revision,source_lineage_json)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,'private','deny',1,'[]')`, artifactID, record.ProjectID,
		"H1 read-only projection", "agent_platform_projection", "application/json", blobRef, len(snapshotRaw), blobRef,
		string(metadata), now, record.OwnerPrincipalID); err != nil {
		return Observation{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_platform_outbox
		(platform_id,platform_run_id,sequence,observation_key,idempotency_key,payload_digest,immutable_envelope,projection_artifact_id,
		 created_at,delivery_status,attempts) VALUES(?,?,?,?,?,?,?,?,?,'pending',0)`, s.trust.PlatformID,
		record.PlatformRunID, sequence, initialObservationKey, envelope.IdempotencyKey, envelope.PayloadDigest,
		envelopeRaw, artifactID, now); err != nil {
		return Observation{}, err
	}
	if result, execErr := tx.ExecContext(ctx, `UPDATE agent_platform_streams SET next_sequence=next_sequence+1,updated_at=?
		WHERE platform_id=? AND platform_run_id=? AND next_sequence=?`, now, s.trust.PlatformID, record.PlatformRunID, sequence); execErr != nil {
		return Observation{}, execErr
	} else if changed, _ := result.RowsAffected(); changed != 1 {
		return Observation{}, fmt.Errorf("%w: stream sequence changed", ErrDigestConflict)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_platform_assignments SET intake_state='observed'
		WHERE platform_id=? AND assignment_id=? AND assignment_revision=?`, s.trust.PlatformID,
		record.AssignmentID, record.AssignmentRevision); err != nil {
		return Observation{}, err
	}
	if err = tx.Commit(); err != nil {
		return Observation{}, err
	}
	return Observation{DecisionActionID: selection.ActionID, DecisionBackend: selection.Backend,
		DecisionLatencyMS: selection.LatencyMS, EnvelopeBytes: envelopeRaw, RunUpdate: update, ProjectionRef: projectionRef}, nil
}

func (s *Service) loadProjection(ctx context.Context, assignmentID string) (assignmentProjection, error) {
	var item assignmentProjection
	err := s.store.DB.QueryRowContext(ctx, `SELECT a.assignment_id,a.assignment_revision,a.platform_run_id,a.harness_task_id,
		a.binding_id,a.binding_revision,b.project_id,b.owner_principal_id
		FROM agent_platform_assignments a JOIN agent_platform_bindings b ON b.binding_id=a.binding_id
		WHERE a.platform_id=? AND a.assignment_id=?`, s.trust.PlatformID, strings.TrimSpace(assignmentID)).Scan(
		&item.AssignmentID, &item.AssignmentRevision, &item.PlatformRunID, &item.HarnessTaskID,
		&item.BindingID, &item.BindingRevision, &item.ProjectID, &item.OwnerPrincipalID)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrUnsupported
	}
	return item, err
}

func (s *Service) existingObservation(ctx context.Context, assignmentID, observationKey string) (Observation, bool, error) {
	var raw []byte
	err := s.store.DB.QueryRowContext(ctx, `SELECT o.immutable_envelope FROM agent_platform_outbox o
		JOIN agent_platform_assignments a ON a.platform_id=o.platform_id AND a.platform_run_id=o.platform_run_id
		WHERE a.platform_id=? AND a.assignment_id=? AND o.observation_key=?`, s.trust.PlatformID,
		strings.TrimSpace(assignmentID), observationKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Observation{}, false, nil
	}
	if err != nil {
		return Observation{}, false, err
	}
	envelope, err := VerifyEnvelope(raw, s.trust, "RunUpdate", RunUpdateRevision)
	if err != nil {
		return Observation{}, false, err
	}
	var update RunUpdate
	if err = decodeStrict(envelope.Payload, &update); err != nil {
		return Observation{}, false, err
	}
	return Observation{EnvelopeBytes: raw, RunUpdate: update, ProjectionRef: update.ProjectionRef}, true, nil
}

func allowedDecisionType(value string) bool {
	return value == "inspect_file" || value == "search_repository" || value == "retrieve_memory"
}

func containsCandidate(items []taskengine.CandidateAction, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}
