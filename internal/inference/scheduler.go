// Package inference owns admission and durable accounting for every provider
// request. It deliberately knows nothing about provider payloads, so prompts,
// tool arguments and credentials cannot enter its queue or metrics.
package inference

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/store"
)

type Priority string

const (
	PriorityForeground    Priority = "foreground"
	PriorityContinuation  Priority = "approval_continuation"
	PriorityTask          Priority = "task"
	PriorityQualification Priority = "qualification"
	PriorityBackground    Priority = "background"
)

type Request struct {
	RequestID                string
	OwnerKind                string
	OwnerID                  string
	SessionID                string
	TurnID                   string
	UsageSource              string
	ResourceKey              string
	Local                    bool
	Priority                 Priority
	PromptTokens             int
	OutputTokens             int
	OwnerTokenLimit          int
	PresetID                 string
	PresetRevision           int
	RuntimeFingerprintID     string
	EffectiveParameterDigest string
	Deadline                 time.Time
}

type Usage struct {
	PromptTokens int
	OutputTokens int
	Quality      string
}

type Metrics struct {
	Queued     int64 `json:"queued"`
	Dispatched int64 `json:"dispatched"`
	Reconciled int64 `json:"reconciled"`
	Cancelled  int64 `json:"cancelled_before_dispatch"`
	Uncertain  int64 `json:"uncertain"`
	QueueMS    int64 `json:"queue_ms"`
	ProviderMS int64 `json:"provider_ms"`
}

var (
	ErrBudgetExceeded      = errors.New("inference token budget exhausted")
	ErrResourceQueueFull   = errors.New("inference resource queue is full")
	ErrResourceQuarantined = errors.New("inference resource is quarantined")
)

type ticket struct {
	id       string
	resource string
	limit    int
	priority int
	seq      uint64
	ready    chan struct{}
	queuedAt time.Time
	granted  bool
	err      error
}

type Scheduler struct {
	store          *store.Store
	mu             sync.Mutex
	globalLimit    int
	globalActive   int
	resourceActive map[string]int
	quarantined    map[string]string
	waiting        []*ticket
	sequence       uint64
	agingInterval  time.Duration
	metrics        Metrics
}

func New(dataStore *store.Store) *Scheduler {
	s := &Scheduler{store: dataStore, globalLimit: 8, resourceActive: map[string]int{}, quarantined: map[string]string{},
		agingInterval: 30 * time.Second}
	if dataStore != nil {
		result, _ := dataStore.DB.Exec(`UPDATE inference_usage_ledger SET state='uncertain',terminal_reason='process_restarted_after_dispatch',updated_at=? WHERE state='dispatched'`, nowText())
		if result != nil {
			s.metrics.Uncertain, _ = result.RowsAffected()
		}
	}
	return s
}

func (s *Scheduler) Do(ctx context.Context, request Request, dispatch func(context.Context) (Usage, error)) (Usage, error) {
	request = normalizeRequest(request)
	if err := s.reserve(ctx, request); err != nil {
		return Usage{}, err
	}
	t, enqueueErr := s.enqueue(request)
	if enqueueErr != nil {
		s.finishBeforeDispatch(request.RequestID, enqueueErr.Error())
		return Usage{}, enqueueErr
	}
	waitDuration := time.Until(request.Deadline)
	if waitDuration < 0 {
		waitDuration = 0
	}
	deadline := time.NewTimer(waitDuration)
	defer deadline.Stop()
	select {
	case <-ctx.Done():
		if s.cancelWaiting(t) {
			s.finishBeforeDispatch(request.RequestID, "context_cancelled")
			return Usage{}, ctx.Err()
		}
		// The grant raced cancellation. Release it without dispatching.
		s.release(t)
		s.finishBeforeDispatch(request.RequestID, "context_cancelled")
		return Usage{}, ctx.Err()
	case <-t.ready:
		if t.err != nil {
			s.finishBeforeDispatch(request.RequestID, t.err.Error())
			return Usage{}, t.err
		}
	case <-deadline.C:
		if s.cancelWaiting(t) {
			s.finishBeforeDispatch(request.RequestID, "queue_deadline_exceeded")
			return Usage{}, context.DeadlineExceeded
		}
		// The grant raced the queue deadline. It has not reached the runtime,
		// so release the logical slot and fail without dispatching.
		s.release(t)
		s.finishBeforeDispatch(request.RequestID, "queue_deadline_exceeded")
		return Usage{}, context.DeadlineExceeded
	}
	queueDuration := time.Since(t.queuedAt)
	if err := s.markDispatched(ctx, request.RequestID); err != nil {
		s.release(t)
		s.markUncertain(request.RequestID, "dispatch_state_persistence_failed")
		return Usage{}, err
	}
	started := time.Now()
	usage, dispatchErr := dispatch(ctx)
	providerDuration := time.Since(started)
	if request.Local && (errors.Is(dispatchErr, context.Canceled) || errors.Is(dispatchErr, context.DeadlineExceeded)) {
		s.quarantine(t, "cancelled_runtime_idle_unconfirmed")
	} else {
		s.release(t)
	}
	if err := s.reconcile(context.WithoutCancel(ctx), request, usage, dispatchErr, providerDuration); err != nil {
		s.markUncertain(request.RequestID, "reconciliation_failed")
		if dispatchErr != nil {
			return usage, dispatchErr
		}
		return usage, fmt.Errorf("provider completed but usage reconciliation failed: %w", err)
	}
	s.mu.Lock()
	s.metrics.QueueMS += queueDuration.Milliseconds()
	s.metrics.ProviderMS += providerDuration.Milliseconds()
	s.metrics.Reconciled++
	s.mu.Unlock()
	return usage, dispatchErr
}

func (s *Scheduler) Metrics() Metrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.metrics
}

func normalizeRequest(request Request) Request {
	if request.RequestID == "" {
		request.RequestID = identity.New("inference")
	}
	if request.OwnerKind == "" {
		request.OwnerKind = "unscoped"
	}
	if request.OwnerID == "" {
		request.OwnerID = "process"
	}
	if request.UsageSource == "" {
		request.UsageSource = "provider"
	}
	if request.ResourceKey == "" {
		request.ResourceKey = "remote:unknown"
	}
	if request.Priority == "" {
		request.Priority = PriorityForeground
	}
	if request.PromptTokens < 0 {
		request.PromptTokens = 0
	}
	if request.OutputTokens < 1 {
		request.OutputTokens = 1
	}
	if request.Deadline.IsZero() {
		request.Deadline = time.Now().Add(10 * time.Minute)
	}
	return request
}

func (s *Scheduler) reserve(ctx context.Context, request Request) error {
	if s.store == nil {
		return nil
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if request.OwnerTokenLimit > 0 {
		var used int
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN state='reconciled' THEN charged_prompt_tokens+charged_output_tokens
			ELSE reserved_prompt_tokens+reserved_output_tokens END),0) FROM inference_usage_ledger
			WHERE owner_kind=? AND owner_id=? AND state IN ('reserved','dispatched','uncertain','reconciled')`, request.OwnerKind, request.OwnerID).Scan(&used)
		if err != nil {
			return err
		}
		if used+request.PromptTokens+request.OutputTokens > request.OwnerTokenLimit {
			return ErrBudgetExceeded
		}
	}
	now := nowText()
	_, err = tx.ExecContext(ctx, `INSERT INTO inference_usage_ledger(id,request_id,owner_kind,owner_id,session_id,turn_id,
		usage_source,resource_key_hash,reserved_prompt_tokens,reserved_output_tokens,usage_quality,preset_id,preset_revision,
		runtime_fingerprint_id,effective_parameter_digest,state,deadline_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'reserved',?,?,?)`, identity.New("usage"), request.RequestID, request.OwnerKind,
		request.OwnerID, nullable(request.SessionID), nullable(request.TurnID), request.UsageSource, hashKey(request.ResourceKey),
		request.PromptTokens, request.OutputTokens, "reserved", request.PresetID, request.PresetRevision,
		request.RuntimeFingerprintID, request.EffectiveParameterDigest, request.Deadline.UTC().Format(time.RFC3339Nano), now, now)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Scheduler) enqueue(request Request) (*ticket, error) {
	limit := 4
	if request.Local {
		limit = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if reason := s.quarantined[request.ResourceKey]; reason != "" {
		return nil, fmt.Errorf("%w: %s", ErrResourceQuarantined, reason)
	}
	if request.Local {
		pending := 0
		for _, candidate := range s.waiting {
			if candidate.resource == request.ResourceKey {
				pending++
			}
		}
		if pending >= 64 {
			return nil, ErrResourceQueueFull
		}
	}
	s.sequence++
	t := &ticket{id: request.RequestID, resource: request.ResourceKey, limit: limit,
		priority: priorityRank(request.Priority), seq: s.sequence, ready: make(chan struct{}), queuedAt: time.Now()}
	s.waiting = append(s.waiting, t)
	s.metrics.Queued++
	s.dispatchLocked()
	return t, nil
}

func (s *Scheduler) dispatchLocked() {
	for s.globalActive < s.globalLimit {
		best := -1
		for index, candidate := range s.waiting {
			if s.quarantined[candidate.resource] != "" {
				continue
			}
			if s.resourceActive[candidate.resource] >= candidate.limit {
				continue
			}
			candidatePriority := s.effectivePriority(candidate)
			bestPriority := 0
			if best >= 0 {
				bestPriority = s.effectivePriority(s.waiting[best])
			}
			if best == -1 || candidatePriority < bestPriority ||
				(candidatePriority == bestPriority && candidate.seq < s.waiting[best].seq) {
				best = index
			}
		}
		if best == -1 {
			return
		}
		t := s.waiting[best]
		s.waiting = append(s.waiting[:best], s.waiting[best+1:]...)
		t.granted = true
		s.globalActive++
		s.resourceActive[t.resource]++
		s.metrics.Dispatched++
		close(t.ready)
	}
}

func (s *Scheduler) effectivePriority(t *ticket) int {
	if s.agingInterval <= 0 {
		return t.priority
	}
	aged := t.priority - int(time.Since(t.queuedAt)/s.agingInterval)
	if aged < 0 {
		return 0
	}
	return aged
}

func (s *Scheduler) cancelWaiting(t *ticket) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.granted {
		return false
	}
	for index, candidate := range s.waiting {
		if candidate == t {
			s.waiting = append(s.waiting[:index], s.waiting[index+1:]...)
			s.metrics.Cancelled++
			return true
		}
	}
	return false
}

func (s *Scheduler) release(t *ticket) {
	s.mu.Lock()
	if t.granted {
		t.granted = false
		s.globalActive--
		s.resourceActive[t.resource]--
	}
	s.dispatchLocked()
	s.mu.Unlock()
}

func (s *Scheduler) quarantine(t *ticket, reason string) {
	s.mu.Lock()
	if t.granted {
		t.granted = false
		s.globalActive--
		s.resourceActive[t.resource]--
	}
	s.quarantined[t.resource] = reason
	remaining := s.waiting[:0]
	for _, candidate := range s.waiting {
		if candidate.resource == t.resource {
			candidate.err = fmt.Errorf("%w: %s", ErrResourceQuarantined, reason)
			close(candidate.ready)
			continue
		}
		remaining = append(remaining, candidate)
	}
	s.waiting = remaining
	s.dispatchLocked()
	s.mu.Unlock()
}

// ReleaseQuarantine is an explicit operator/runtime-reconfiguration action.
// Cancellation alone never claims that a possibly busy local backend is idle.
func (s *Scheduler) ReleaseQuarantine(resourceKey string) {
	s.mu.Lock()
	delete(s.quarantined, resourceKey)
	s.dispatchLocked()
	s.mu.Unlock()
}

func (s *Scheduler) markDispatched(ctx context.Context, requestID string) error {
	if s.store == nil {
		return nil
	}
	result, err := s.store.DB.ExecContext(ctx, `UPDATE inference_usage_ledger SET state='dispatched',active_started_at=?,updated_at=? WHERE request_id=? AND state='reserved'`, nowText(), nowText(), requestID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return fmt.Errorf("inference reservation %s is not dispatchable", requestID)
	}
	return nil
}

func (s *Scheduler) finishBeforeDispatch(requestID, reason string) {
	if s.store != nil {
		_, _ = s.store.DB.Exec(`UPDATE inference_usage_ledger SET state='cancelled_before_dispatch',terminal_reason=?,updated_at=? WHERE request_id=? AND state='reserved'`, reason, nowText(), requestID)
	}
}

func (s *Scheduler) reconcile(ctx context.Context, request Request, usage Usage, dispatchErr error, elapsed time.Duration) error {
	if s.store == nil {
		return nil
	}
	quality := usage.Quality
	if quality == "" {
		quality = "actual"
	}
	prompt, output := usage.PromptTokens, usage.OutputTokens
	if prompt+output == 0 {
		prompt, output, quality = request.PromptTokens, request.OutputTokens, "conservative_estimate"
	}
	reason := "completed"
	if dispatchErr != nil {
		reason = "provider_error"
	}
	result, err := s.store.DB.ExecContext(ctx, `UPDATE inference_usage_ledger SET state='reconciled',charged_prompt_tokens=?,charged_output_tokens=?,
		usage_quality=?,active_elapsed_ms=?,terminal_reason=?,updated_at=? WHERE request_id=? AND state='dispatched'`,
		prompt, output, quality, elapsed.Milliseconds(), reason, nowText(), request.RequestID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Scheduler) markUncertain(requestID, reason string) {
	if s.store != nil {
		_, _ = s.store.DB.Exec(`UPDATE inference_usage_ledger SET state='uncertain',terminal_reason=?,updated_at=? WHERE request_id=? AND state IN ('reserved','dispatched')`, reason, nowText(), requestID)
	}
	s.mu.Lock()
	s.metrics.Uncertain++
	s.mu.Unlock()
}

func priorityRank(priority Priority) int {
	switch priority {
	case PriorityForeground, PriorityContinuation:
		return 0
	case PriorityTask:
		return 1
	case PriorityQualification:
		return 2
	default:
		return 3
	}
}

func hashKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func nowText() string { return time.Now().UTC().Format(time.RFC3339Nano) }
