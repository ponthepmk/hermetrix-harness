package agentplatform_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"hermetrix-harness/internal/agentplatform"
	"hermetrix-harness/internal/agentplatform/fixture"
	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/taskengine"
)

type h1Fixture struct {
	ctx        context.Context
	store      *store.Store
	service    *agentplatform.Service
	trust      agentplatform.Trust
	binding    agentplatform.Binding
	assignment agentplatform.TaskAssignment
	raw        []byte
}

func newH1Fixture(t *testing.T) *h1Fixture {
	t.Helper()
	ctx := context.Background()
	base := t.TempDir()
	repo := filepath.Join(base, "fixture-repository")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	dataStore, err := store.Open(ctx, filepath.Join(base, "fixture-data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })
	if err = os.WriteFile(filepath.Join(base, ".hermetrix-h1-fixture"), []byte("fixture-only\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	productService := product.NewService(dataStore, nil)
	t.Cleanup(productService.Close)
	project, err := productService.SaveProject(ctx, product.ProjectInput{Name: "H1 fixture", RootPath: repo})
	if err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	trust := agentplatform.Trust{PlatformID: "platform-fixture", NodeID: "node-fixture", AgentID: "agent-fixture",
		KeyID: "fixture-key-1", PublicKey: public, PrivateKey: private, Now: func() time.Time { return now }}
	service, err := agentplatform.New(dataStore, taskengine.NewService(dataStore), trust)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := service.RegisterBinding(ctx, "repository-fixture", nil, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	scope := agentplatform.AccessScope{Access: "read", RepositoryID: "repository-fixture"}
	claim := agentplatform.AssignmentAuthorizationClaim{AssignmentID: "assignment-1", AssignmentRevision: 1,
		NodeID: trust.NodeID, AgentID: trust.AgentID, PlatformRunID: "platform-run-1", RepositoryID: "repository-fixture",
		AccessScope: scope, IssuedAt: "2026-09-22T11:59:00.000Z", ExpiresAt: "2026-09-22T12:10:00.000Z",
		Integrity: agentplatform.ClaimIntegrity{Alg: "ed25519"}}
	claim, err = agentplatform.SignClaim(claim, private)
	if err != nil {
		t.Fatal(err)
	}
	assignment := agentplatform.TaskAssignment{ContractVersion: agentplatform.ContractVersion,
		SchemaRevision: agentplatform.TaskAssignmentRevision, AssignmentID: claim.AssignmentID, AssignmentRevision: 1,
		PlatformRunID: claim.PlatformRunID, AgentID: trust.AgentID, NodeID: trust.NodeID,
		RepositoryID: scope.RepositoryID, TaskID: "platform-task-1", Title: "Inspect without execution",
		OriginalRequest: "Record a read-only decision", Goal: "Prove the read-only integration boundary",
		AcceptanceCriteria: []agentplatform.AcceptanceCriterion{{ID: "AC-1", Description: "No execution occurs"}},
		AccessScope:        scope, AuthorizationClaim: claim, IssuedAt: "2026-09-22T12:00:00.000Z"}
	raw, _, err := agentplatform.SignEnvelope("TaskAssignment", agentplatform.TaskAssignmentRevision, trust.NodeID,
		"assignment-1:1", assignment, trust.KeyID, private)
	if err != nil {
		t.Fatal(err)
	}
	return &h1Fixture{ctx: ctx, store: dataStore, service: service, trust: trust, binding: binding,
		assignment: assignment, raw: raw}
}

func TestH1IntakeObserveIsIdempotentAndCreatesNoExecutionState(t *testing.T) {
	f := newH1Fixture(t)
	first, err := f.service.Intake(f.ctx, f.raw)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.Intake(f.ctx, f.raw)
	if err != nil || first.HarnessTaskID != second.HarnessTaskID {
		t.Fatalf("exact duplicate changed projection: first=%+v second=%+v err=%v", first, second, err)
	}
	observation, err := f.service.ObserveInitial(f.ctx, first.AssignmentID)
	if err != nil {
		t.Fatal(err)
	}
	if observation.DecisionActionID != "retrieve_memory" || observation.RunUpdate.ExecutionStatus != "accepted" ||
		observation.RunUpdate.HarnessRunRef != nil || observation.RunUpdate.Progress.TotalSteps != 0 ||
		observation.RunUpdate.Progress.AttemptCount != 0 || observation.RunUpdate.Progress.FailedChecks != 0 {
		t.Fatalf("invented or unsafe observation facts: %+v", observation)
	}
	replayed, err := f.service.ObserveInitial(f.ctx, first.AssignmentID)
	if err != nil || !bytes.Equal(observation.EnvelopeBytes, replayed.EnvelopeBytes) {
		t.Fatalf("observation replay rebuilt bytes: %v", err)
	}
	if f.service.DecisionInvocationCount() != 1 {
		t.Fatalf("decision invocations = %d, want 1", f.service.DecisionInvocationCount())
	}
	for _, table := range []string{"task_runs", "task_step_attempts", "task_effect_intents", "task_validations"} {
		var count int
		if err = f.store.DB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want 0", table, count)
		}
	}
	var tasks, assignments, outbox, artifacts int
	for query, target := range map[string]*int{
		`SELECT COUNT(*) FROM durable_tasks`:                                    &tasks,
		`SELECT COUNT(*) FROM agent_platform_assignments`:                       &assignments,
		`SELECT COUNT(*) FROM agent_platform_outbox`:                            &outbox,
		`SELECT COUNT(*) FROM artifacts WHERE kind='agent_platform_projection'`: &artifacts,
	} {
		if err = f.store.DB.QueryRow(query).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if tasks != 1 || assignments != 1 || outbox != 1 || artifacts != 1 {
		t.Fatalf("unexpected durable counts task=%d assignment=%d outbox=%d artifact=%d", tasks, assignments, outbox, artifacts)
	}
}

func TestH1ConcurrentDuplicateAndExpiredHistoricalReplay(t *testing.T) {
	f := newH1Fixture(t)
	const workers = 8
	results := make(chan agentplatform.AssignmentReceipt, workers)
	errorsSeen := make(chan error, workers)
	var group sync.WaitGroup
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			receipt, err := f.service.Intake(f.ctx, f.raw)
			if err != nil {
				errorsSeen <- err
				return
			}
			results <- receipt
		}()
	}
	group.Wait()
	close(results)
	close(errorsSeen)
	for err := range errorsSeen {
		t.Fatal(err)
	}
	var taskID string
	for result := range results {
		if taskID == "" {
			taskID = result.HarnessTaskID
		}
		if result.HarnessTaskID != taskID {
			t.Fatalf("concurrent replay created task %s after %s", result.HarnessTaskID, taskID)
		}
	}
	var count int
	if err := f.store.DB.QueryRow(`SELECT COUNT(*) FROM durable_tasks`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("durable task count=%d err=%v", count, err)
	}
	expiredTrust := f.trust
	expiredTrust.Now = func() time.Time { return time.Date(2026, 9, 22, 13, 0, 0, 0, time.UTC) }
	restarted, err := agentplatform.New(f.store, taskengine.NewService(f.store), expiredTrust)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.Intake(f.ctx, f.raw); err != nil {
		t.Fatalf("authenticated historical replay was rejected after claim expiry: %v", err)
	}
}

func TestH1DuplicateAcrossServiceInstancesIsAtomicAndIdempotent(t *testing.T) {
	f := newH1Fixture(t)
	secondService, err := agentplatform.New(f.store, taskengine.NewService(f.store), f.trust)
	if err != nil {
		t.Fatal(err)
	}
	services := []*agentplatform.Service{f.service, secondService}
	results := make(chan agentplatform.AssignmentReceipt, len(services))
	errorsSeen := make(chan error, len(services))
	var group sync.WaitGroup
	for _, service := range services {
		group.Add(1)
		go func(current *agentplatform.Service) {
			defer group.Done()
			receipt, intakeErr := current.Intake(f.ctx, f.raw)
			if intakeErr != nil {
				errorsSeen <- intakeErr
				return
			}
			results <- receipt
		}(service)
	}
	group.Wait()
	close(results)
	close(errorsSeen)
	for intakeErr := range errorsSeen {
		t.Fatal(intakeErr)
	}
	var taskID string
	for receipt := range results {
		if taskID == "" {
			taskID = receipt.HarnessTaskID
		}
		if receipt.HarnessTaskID != taskID {
			t.Fatalf("service instances returned different tasks: %s != %s", receipt.HarnessTaskID, taskID)
		}
	}
	var tasks int
	if err = f.store.DB.QueryRow(`SELECT COUNT(*) FROM durable_tasks`).Scan(&tasks); err != nil || tasks != 1 {
		t.Fatalf("durable tasks=%d err=%v", tasks, err)
	}
}

func TestH1RejectsDigestConflictWriteAndBindingDrift(t *testing.T) {
	f := newH1Fixture(t)
	if _, err := f.service.Intake(f.ctx, f.raw); err != nil {
		t.Fatal(err)
	}
	changed := f.assignment
	changed.Title = "different immutable payload"
	raw, _, err := agentplatform.SignEnvelope("TaskAssignment", agentplatform.TaskAssignmentRevision, f.trust.NodeID,
		"assignment-1:changed", changed, f.trust.KeyID, ed25519.PrivateKey(f.trust.PrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Intake(f.ctx, raw); !errors.Is(err, agentplatform.ErrDigestConflict) {
		t.Fatalf("digest conflict error = %v", err)
	}
	write := f.assignment
	write.AssignmentID = "assignment-write"
	write.PlatformRunID = "platform-run-write"
	write.TaskID = "platform-task-write"
	write.AccessScope.Access = "write"
	authority := "authority-1"
	generation := 1
	write.AuthorityScopeID, write.AuthorityGeneration = &authority, &generation
	write.AuthorizationClaim.AssignmentID = write.AssignmentID
	write.AuthorizationClaim.PlatformRunID = write.PlatformRunID
	write.AuthorizationClaim.AccessScope = write.AccessScope
	write.AuthorizationClaim.AuthorityScopeID, write.AuthorizationClaim.AuthorityGeneration = &authority, &generation
	write.AuthorizationClaim, err = agentplatform.SignClaim(write.AuthorizationClaim, ed25519.PrivateKey(f.trust.PrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	writeRaw, _, err := agentplatform.SignEnvelope("TaskAssignment", agentplatform.TaskAssignmentRevision, f.trust.NodeID,
		"assignment-write:1", write, f.trust.KeyID, ed25519.PrivateKey(f.trust.PrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Intake(f.ctx, writeRaw); !errors.Is(err, agentplatform.ErrWriteUnsupported) {
		t.Fatalf("write assignment error = %v", err)
	}
	if _, err = f.store.DB.Exec(`UPDATE projects SET root_path=? WHERE id=?`, t.TempDir(), f.binding.ProjectID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.ObserveInitial(f.ctx, f.assignment.AssignmentID); !errors.Is(err, agentplatform.ErrBinding) {
		t.Fatalf("binding drift error = %v", err)
	}
}

type loseFirstACK struct {
	receiver *fixture.Receiver
	lost     bool
}

func (l *loseFirstACK) Send(ctx context.Context, raw []byte) ([]byte, error) {
	ack, err := l.receiver.Send(ctx, raw)
	if err != nil {
		return nil, err
	}
	if !l.lost {
		l.lost = true
		return nil, errors.New("simulated ACK loss")
	}
	return ack, nil
}

func TestH1DurableDeliverySurvivesACKLossAndReceiverRestart(t *testing.T) {
	f := newH1Fixture(t)
	receipt, err := f.service.Intake(f.ctx, f.raw)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := f.service.ObserveInitial(f.ctx, receipt.AssignmentID)
	if err != nil {
		t.Fatal(err)
	}
	receiverTrust := newTrust(t, "fixture-receiver")
	receiverRoot := filepath.Join(t.TempDir(), "receiver")
	receiver, err := fixture.OpenReceiver(f.ctx, receiverRoot, f.trust, receiverTrust)
	if err != nil {
		t.Fatal(err)
	}
	lossy := &loseFirstACK{receiver: receiver}
	result, err := f.service.DeliverPending(f.ctx, receiverTrust, lossy, 3)
	if err != nil || result.Pending != 1 {
		t.Fatalf("ACK loss result=%+v err=%v", result, err)
	}
	if err = receiver.Close(); err != nil {
		t.Fatal(err)
	}
	receiver, err = fixture.OpenReceiver(f.ctx, receiverRoot, f.trust, receiverTrust)
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	restartedSender, err := agentplatform.New(f.store, taskengine.NewService(f.store), f.trust)
	if err != nil {
		t.Fatal(err)
	}
	result, err = restartedSender.DeliverPending(f.ctx, receiverTrust, receiver, 3)
	if err != nil || result.Pending != 0 || result.Acked != observation.RunUpdate.Sequence {
		t.Fatalf("restart delivery result=%+v err=%v", result, err)
	}
	acked, err := receiver.AckedSequence(f.ctx, receipt.PlatformRunID)
	if err != nil || acked != 1 {
		t.Fatalf("receiver ack=%d err=%v", acked, err)
	}
}

type alwaysFailTransport struct{ calls int }

func (a *alwaysFailTransport) Send(context.Context, []byte) ([]byte, error) {
	a.calls++
	return nil, errors.New("fixture receiver unavailable")
}

func TestH1RetryExhaustionRetainsAcceptedTaskAndOutbox(t *testing.T) {
	f := newH1Fixture(t)
	receipt, err := f.service.Intake(f.ctx, f.raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.ObserveInitial(f.ctx, receipt.AssignmentID); err != nil {
		t.Fatal(err)
	}
	transport := &alwaysFailTransport{}
	receiverTrust := newTrust(t, "unavailable-receiver")
	for i := 0; i < 3; i++ {
		if _, err = f.service.DeliverPending(f.ctx, receiverTrust, transport, 3); err != nil {
			t.Fatal(err)
		}
	}
	result, err := f.service.DeliverPending(f.ctx, receiverTrust, transport, 3)
	if err != nil || result.Exhausted != 1 || result.Pending != 1 || transport.calls != 3 {
		t.Fatalf("retry exhaustion result=%+v calls=%d err=%v", result, transport.calls, err)
	}
	var state, executionStatus string
	if err = f.store.DB.QueryRow(`SELECT state FROM durable_tasks WHERE id=?`, receipt.HarnessTaskID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err = f.store.DB.QueryRow(`SELECT immutable_envelope FROM agent_platform_outbox`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	_, update, err := agentplatform.ValidateRunUpdate(raw, f.trust)
	if err != nil {
		t.Fatal(err)
	}
	executionStatus = update.ExecutionStatus
	if state != taskengine.StateDraft || executionStatus != "accepted" {
		t.Fatalf("transport failure changed execution state: task=%s update=%s", state, executionStatus)
	}
}

func TestH1DatabaseGuardAndInvocationCanaries(t *testing.T) {
	f := newH1Fixture(t)
	_, err := f.store.DB.Exec(`INSERT INTO background_jobs(id,kind,state,progress) VALUES('forbidden','command','queued',0)`)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("H1 forbids background jobs")) {
		t.Fatalf("DB execution backstop did not fire: %v", err)
	}
	counters := f.service.InvocationCounters()
	if counters.PlanEffect != 0 || counters.DispatchEffect != 0 || counters.BeginRun != 0 || counters.BeginStepAttempt != 0 ||
		counters.Command != 0 || counters.Provider != 0 || counters.Tool != 0 || counters.WorkspaceWrite != 0 ||
		counters.Planner != 0 || counters.AgentTurn != 0 {
		t.Fatalf("forbidden invocation canary changed: %+v", counters)
	}
}

func TestFixtureReceiverContiguousACKAndConflict(t *testing.T) {
	ctx := context.Background()
	sender := newTrust(t, "sender")
	receiverTrust := newTrust(t, "receiver")
	receiver, err := fixture.OpenReceiver(ctx, t.TempDir(), sender, receiverTrust)
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	for i, want := range []struct{ seq, ack int64 }{{1, 1}, {3, 1}, {2, 3}} {
		raw := signedUpdate(t, sender, "ordered-run", want.seq, "accepted")
		ackRaw, err := receiver.Send(ctx, raw)
		if err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
		_, ack, err := agentplatform.ValidateACK(ackRaw, receiverTrust)
		if err != nil || ack.AckedSequence != want.ack {
			t.Fatalf("sequence %d ack=%+v err=%v", want.seq, ack, err)
		}
	}
	if _, err = receiver.Send(ctx, signedUpdate(t, sender, "ordered-run", 2, "initializing")); !errors.Is(err, agentplatform.ErrDigestConflict) {
		t.Fatalf("same sequence/different digest error=%v", err)
	}
}

func newTrust(t *testing.T, node string) agentplatform.Trust {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	return agentplatform.Trust{PlatformID: "platform-fixture", NodeID: node, AgentID: node + "-agent",
		KeyID: node + "-key", PublicKey: public, PrivateKey: private, Now: func() time.Time { return now }}
}

func signedUpdate(t *testing.T, trust agentplatform.Trust, run string, sequence int64, status string) []byte {
	t.Helper()
	now := "2026-09-22T12:00:00.000Z"
	update := agentplatform.RunUpdate{PlatformRunID: run, Sequence: sequence, ExecutionStatus: status,
		Progress: agentplatform.ProgressFacts{Phase: "initializing", BlockedReasons: []string{}},
		ProjectionRef: agentplatform.EvidenceRef{EvidenceID: "artifact-1", Type: "artifact", OriginNodeID: trust.NodeID,
			ContentDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CreatedAt: now}, CreatedAt: now}
	raw, _, err := agentplatform.SignEnvelope("RunUpdate", agentplatform.RunUpdateRevision, trust.NodeID,
		"update:"+run+":"+time.Unix(sequence, 0).UTC().String()+":"+status, update, trust.KeyID,
		ed25519.PrivateKey(trust.PrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
