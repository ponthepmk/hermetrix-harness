package learning

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/runtime"
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
)

var ErrNoQueuedReview = errors.New("no queued learning review")

type Service struct {
	store    *store.Store
	skills   *skills.Service
	gate     *runtime.InferenceGate
	reviewer Reviewer
}

func NewService(dataStore *store.Store, skillService *skills.Service, gate *runtime.InferenceGate, reviewer Reviewer) *Service {
	return &Service{store: dataStore, skills: skillService, gate: gate, reviewer: reviewer}
}

// WithReviewer swaps the review worker once a provider is known. Reviews are
// enqueued from the first turn, so the service has to exist before any provider
// does; without this the learner would be fixed at startup to the one reviewer
// that cannot read evidence.
func (s *Service) WithReviewer(reviewer Reviewer) *Service {
	if reviewer != nil {
		s.reviewer = reviewer
	}
	return s
}

// ReviewerRevision reports which worker will handle the next review.
func (s *Service) ReviewerRevision() string {
	if s.reviewer == nil {
		return ""
	}
	return s.reviewer.Revision()
}

func (s *Service) RecoverInterrupted(ctx context.Context) (int64, error) {
	result, err := s.store.DB.ExecContext(ctx, `UPDATE learning_reviews SET state=?, error=?, started_at=NULL
		WHERE state=?`, StateQueued, "requeued after process interruption", StateRunning)
	if err != nil {
		return 0, err
	}
	reviews, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	outbox, err := s.store.DB.ExecContext(ctx, `UPDATE learning_trigger_outbox SET state='pending',
		error='requeued after process interruption' WHERE state='processing'`)
	if err != nil {
		return 0, err
	}
	requeued, err := outbox.RowsAffected()
	return reviews + requeued, err
}

// StageTrigger writes a learning trigger into the same transaction that
// commits the observed turn outcome. The background reviewer can therefore be
// delayed or restarted without losing the evidence-to-review edge.
func (s *Service) StageTrigger(ctx context.Context, tx *sql.Tx, input EnqueueInput) (bool, error) {
	if tx == nil {
		return false, fmt.Errorf("learning trigger requires a transaction")
	}
	if err := validateTriggerPolicy(input); err != nil {
		return false, err
	}
	if strings.TrimSpace(input.SessionID) == "" || strings.TrimSpace(input.MilestoneID) == "" {
		return false, fmt.Errorf("learning trigger requires session and milestone")
	}
	digestJSON, err := json.Marshal(input.Digest)
	if err != nil {
		return false, err
	}
	turnID := strings.TrimSpace(input.TurnID)
	if turnID == "" {
		turnID = input.MilestoneID
	}
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO learning_trigger_outbox
		(id,session_id,turn_id,milestone_id,job_id,trigger_kind,digest_json,state,created_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, identity.New("learnout"), input.SessionID, turnID,
		input.MilestoneID, input.JobID, input.TriggerKind, string(digestJSON), "pending", formatTime(time.Now().UTC()))
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
}

// DrainPending converts committed outbox records into idempotent review jobs.
// It never runs the reviewer itself and is safe to call after every foreground
// turn as well as from a periodic background ticker.
func (s *Service) DrainPending(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	type staged struct {
		id, sessionID, milestoneID, jobID, triggerKind, digestJSON string
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id,session_id,milestone_id,job_id,trigger_kind,digest_json
		FROM learning_trigger_outbox WHERE state='pending' ORDER BY created_at LIMIT ?`, limit)
	if err != nil {
		return 0, err
	}
	var items []staged
	for rows.Next() {
		var item staged
		if err := rows.Scan(&item.id, &item.sessionID, &item.milestoneID, &item.jobID, &item.triggerKind, &item.digestJSON); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	processed := 0
	for _, item := range items {
		claim, err := s.store.DB.ExecContext(ctx, `UPDATE learning_trigger_outbox SET state='processing',error=''
			WHERE id=? AND state='pending'`, item.id)
		if err != nil {
			return processed, err
		}
		if changed, _ := claim.RowsAffected(); changed != 1 {
			continue
		}
		var digest Digest
		if err := json.Unmarshal([]byte(item.digestJSON), &digest); err != nil {
			_, _ = s.store.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE learning_trigger_outbox
				SET state='failed',error=?,processed_at=? WHERE id=? AND state='processing'`,
				"decode staged digest: "+err.Error(), formatTime(time.Now().UTC()), item.id)
			continue
		}
		_, _, enqueueErr := s.Enqueue(ctx, EnqueueInput{SessionID: item.sessionID, JobID: item.jobID,
			MilestoneID: item.milestoneID, TriggerKind: item.triggerKind, Digest: digest})
		if enqueueErr != nil {
			_, _ = s.store.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE learning_trigger_outbox
				SET state='pending',error=? WHERE id=? AND state='processing'`, enqueueErr.Error(), item.id)
			return processed, enqueueErr
		}
		result, err := s.store.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE learning_trigger_outbox
			SET state='processed',error='',processed_at=? WHERE id=? AND state='processing'`, formatTime(time.Now().UTC()), item.id)
		if err != nil {
			return processed, err
		}
		if changed, _ := result.RowsAffected(); changed == 1 {
			processed++
		}
	}
	return processed, nil
}

func (s *Service) Enqueue(ctx context.Context, input EnqueueInput) (Job, bool, error) {
	if strings.TrimSpace(input.SessionID) == "" || strings.TrimSpace(input.MilestoneID) == "" || strings.TrimSpace(input.TriggerKind) == "" {
		return Job{}, false, fmt.Errorf("session, milestone, and trigger are required")
	}
	if err := validateTriggerPolicy(input); err != nil {
		return Job{}, false, err
	}
	digestJSON, err := json.Marshal(input.Digest)
	if err != nil {
		return Job{}, false, err
	}
	keySum := sha256.Sum256([]byte(input.SessionID + "|" + input.MilestoneID + "|" + s.reviewer.Revision()))
	key := hex.EncodeToString(keySum[:])
	if existing, err := s.getByKey(ctx, key); err == nil {
		return existing, true, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, err
	}
	now := time.Now().UTC()
	job := Job{ID: identity.New("review"), IdempotencyKey: key, State: StateQueued,
		TriggerKind: input.TriggerKind, SessionID: input.SessionID, JobID: input.JobID,
		Digest: input.Digest, ReviewerRevision: s.reviewer.Revision(), CreatedAt: now}
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO learning_reviews(id,idempotency_key,state,trigger_kind,
		session_id,job_id,digest_json,reviewer_revision,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		job.ID, job.IdempotencyKey, job.State, job.TriggerKind, job.SessionID, job.JobID,
		string(digestJSON), job.ReviewerRevision, formatTime(now))
	if err != nil {
		if existing, findErr := s.getByKey(ctx, key); findErr == nil {
			return existing, true, nil
		}
		return Job{}, false, err
	}
	return job, false, nil
}

func validateTriggerPolicy(input EnqueueInput) error {
	outcome := strings.ToLower(strings.TrimSpace(input.Digest.Outcome))
	switch input.TriggerKind {
	case "successful_milestone":
		if outcome != "success" && outcome != "completed" {
			return fmt.Errorf("successful_milestone requires an observed successful outcome")
		}
	case "repeated_correction":
		if len(cleanEvidence(input.Digest.UserCorrections)) < 2 {
			return fmt.Errorf("repeated_correction requires at least two concrete corrections")
		}
	case "explicit_learn":
		// The explicit user request is the authority boundary.
	case "skill_failure":
		if outcome != "failure" || len(cleanEvidence(input.Digest.SkillActivations)) == 0 {
			return fmt.Errorf("skill_failure requires a failed outcome and at least one activation receipt")
		}
	case "batch":
		if input.Digest.SuggestedSkill != nil {
			return fmt.Errorf("batch reviews cannot carry a direct skill mutation suggestion")
		}
	default:
		return fmt.Errorf("unsupported learning trigger %q", input.TriggerKind)
	}
	return nil
}

func cleanEvidence(values []string) []string {
	out := values[:0]
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}

func (s *Service) List(ctx context.Context, state string) ([]Job, error) {
	query := reviewSelect
	args := []any{}
	if state != "" {
		query += ` WHERE state=?`
		args = append(args, state)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.store.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Job, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *Service) RunNext(ctx context.Context) (Job, error) {
	job, err := s.claimNext(ctx)
	if err != nil {
		return Job{}, err
	}
	err = s.gate.RunBackground(ctx, func(reviewCtx context.Context) error {
		decision, reviewErr := s.reviewer.Review(reviewCtx, job.Digest)
		if reviewErr != nil {
			return reviewErr
		}
		job.Decision = decision
		// The reviewer's own proposal wins; the digest's is the API path, where
		// a caller hands over markdown it already wrote.
		suggestion := decision.SuggestedSkill
		if suggestion == nil {
			suggestion = job.Digest.SuggestedSkill
		}
		if decision.Kind == "no_change" || suggestion == nil {
			return nil
		}
		if existing, findErr := s.skills.GetCandidateBySourceReview(reviewCtx, job.ID); findErr == nil {
			job.CandidateID = existing.ID
			return nil
		} else if !errors.Is(findErr, skills.ErrNotFound) {
			return findErr
		}
		suggested := suggestion
		origin := "agent_candidate"
		if suggested.TargetSkillID != "" {
			target, targetErr := s.skills.GetSkill(reviewCtx, suggested.TargetSkillID)
			if targetErr != nil {
				return targetErr
			}
			suggested.CanonicalName = target.CanonicalName
			suggested.ScopeKind = target.ScopeKind
			suggested.ScopeRef = target.ScopeRef
			suggested.Owner = target.Owner
			suggested.BaseVersionID = target.CurrentVersionID
			origin = target.Origin
		}
		candidate, createErr := s.skills.CreateCandidate(reviewCtx, skills.CreateCandidateInput{
			CanonicalName: suggested.CanonicalName, ScopeKind: suggested.ScopeKind, ScopeRef: suggested.ScopeRef,
			Origin: origin, Owner: suggested.Owner, ChangeKind: suggested.ChangeKind,
			TargetSkillID: suggested.TargetSkillID, BaseVersionID: suggested.BaseVersionID,
			CreatedBy: "background_reviewer", TriggerKind: job.TriggerKind, Reason: suggested.Reason,
			EvidenceRefs: []string{"review:" + job.ID, "session:" + job.SessionID, "job:" + job.JobID},
			Markdown:     suggested.Markdown, SourceReviewID: job.ID})
		if createErr != nil {
			return createErr
		}
		job.CandidateID = candidate.ID
		if _, authorityErr := s.skills.TryAutomatedPromotion(reviewCtx, candidate.ID); authorityErr != nil {
			return fmt.Errorf("apply configured Skill authority policy: %w", authorityErr)
		}
		return nil
	})
	persistCtx := context.WithoutCancel(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			_ = s.requeue(persistCtx, job.ID, "preempted by foreground work")
			return s.get(persistCtx, job.ID)
		}
		_ = s.fail(persistCtx, job.ID, err.Error())
		return s.get(persistCtx, job.ID)
	}
	decisionJSON, _ := json.Marshal(job.Decision)
	now := time.Now().UTC()
	_, err = s.store.DB.ExecContext(persistCtx, `UPDATE learning_reviews SET state=?, decision_json=?, candidate_id=?,
		error='', completed_at=? WHERE id=? AND state=?`, StateCompleted, string(decisionJSON), nullIfEmpty(job.CandidateID),
		formatTime(now), job.ID, StateRunning)
	if err != nil {
		return Job{}, err
	}
	return s.get(persistCtx, job.ID)
}

func (s *Service) claimNext(ctx context.Context) (Job, error) {
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	var id string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM learning_reviews WHERE state=? ORDER BY created_at LIMIT 1`, StateQueued).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Job{}, ErrNoQueuedReview
		}
		return Job{}, err
	}
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE learning_reviews SET state=?, attempts=attempts+1,
		started_at=?, error='' WHERE id=? AND state=?`, StateRunning, formatTime(now), id, StateQueued)
	if err != nil {
		return Job{}, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return Job{}, ErrNoQueuedReview
	}
	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	return s.get(ctx, id)
}

func (s *Service) requeue(ctx context.Context, id, reason string) error {
	_, err := s.store.DB.ExecContext(ctx, `UPDATE learning_reviews SET state=?, error=?, started_at=NULL WHERE id=? AND state=?`, StateQueued, reason, id, StateRunning)
	return err
}

func (s *Service) fail(ctx context.Context, id, reason string) error {
	_, err := s.store.DB.ExecContext(ctx, `UPDATE learning_reviews SET state=?, error=?, completed_at=? WHERE id=? AND state=?`, StateFailed, reason, formatTime(time.Now().UTC()), id, StateRunning)
	return err
}

func (s *Service) get(ctx context.Context, id string) (Job, error) {
	return scanJob(s.store.DB.QueryRowContext(ctx, reviewSelect+` WHERE id=?`, id))
}

func (s *Service) getByKey(ctx context.Context, key string) (Job, error) {
	return scanJob(s.store.DB.QueryRowContext(ctx, reviewSelect+` WHERE idempotency_key=?`, key))
}

const reviewSelect = `SELECT id,idempotency_key,state,trigger_kind,session_id,job_id,digest_json,
	reviewer_revision,decision_json,COALESCE(candidate_id,''),attempts,error,created_at,started_at,completed_at FROM learning_reviews`

type scanner interface{ Scan(...any) error }

func scanJob(row scanner) (Job, error) {
	var job Job
	var digestJSON, decisionJSON, created string
	var started, completed sql.NullString
	if err := row.Scan(&job.ID, &job.IdempotencyKey, &job.State, &job.TriggerKind, &job.SessionID,
		&job.JobID, &digestJSON, &job.ReviewerRevision, &decisionJSON, &job.CandidateID,
		&job.Attempts, &job.Error, &created, &started, &completed); err != nil {
		return Job{}, err
	}
	_ = json.Unmarshal([]byte(digestJSON), &job.Digest)
	_ = json.Unmarshal([]byte(decisionJSON), &job.Decision)
	job.CreatedAt, _ = parseTime(created)
	if started.Valid {
		value, _ := parseTime(started.String)
		job.StartedAt = &value
	}
	if completed.Valid {
		value, _ := parseTime(completed.String)
		job.CompletedAt = &value
	}
	return job, nil
}

func formatTime(value time.Time) string         { return value.UTC().Format(time.RFC3339Nano) }
func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }
func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
