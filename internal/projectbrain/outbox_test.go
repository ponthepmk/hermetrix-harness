package projectbrain

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"hermetrix-harness/internal/store"
)

type outboxSubmitter struct {
	calls int
	err   error
}

func (s *outboxSubmitter) SubmitCandidate(_ context.Context, payload json.RawMessage) (SubmissionReceipt, error) {
	s.calls++
	if s.err != nil {
		return SubmissionReceipt{}, s.err
	}
	var value struct {
		CandidateID   string `json:"candidate_id"`
		PayloadDigest string `json:"payload_digest"`
	}
	_ = json.Unmarshal(payload, &value)
	return SubmissionReceipt{Status: "submitted_unverified", CandidateID: value.CandidateID,
		PayloadDigest: value.PayloadDigest, Commit: "abc123"}, nil
}

func testOutbox(t *testing.T) (*store.Store, *CandidateOutbox, *outboxSubmitter) {
	t.Helper()
	data, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	remote := &outboxSubmitter{}
	outbox := &CandidateOutbox{DB: data.DB, Submitter: remote,
		Authorize: func(context.Context, CandidateMessage) error { return nil }, Now: func() time.Time {
			return time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
		}}
	return data, outbox, remote
}

func testCandidate() CandidateMessage {
	task, check, approval := candidateTestFacts()
	candidate, _ := BuildKnowledgeCandidate(task, check, approval)
	payload, _ := json.Marshal(candidate)
	return CandidateMessage{CandidateID: candidate.CandidateID, ProjectID: candidate.ProjectID,
		TaskID: task.HarnessTaskID, PayloadDigest: candidate.PayloadDigest,
		Payload: payload, Approval: approval}
}

func TestCandidateOutboxRetryAndIdempotency(t *testing.T) {
	ctx := context.Background()
	data, outbox, remote := testOutbox(t)
	msg := testCandidate()
	queued, err := outbox.Queue(ctx, msg)
	if err != nil || queued.State != "pending" {
		t.Fatalf("queue: %+v %v", queued, err)
	}
	if _, err = outbox.Queue(ctx, msg); err != nil {
		t.Fatalf("same candidate must be idempotent: %v", err)
	}
	remote.err = errors.New("Pi unavailable; secret-bearing detail must not be stored")
	if count, err := outbox.Drain(ctx, 10); err != nil || count != 0 {
		t.Fatalf("unavailable Pi must not mark delivery successful: %d %v", count, err)
	}
	status, err := outbox.Get(ctx, msg.CandidateID)
	if err != nil || status.State != "pending" || status.LastError != "submit_unavailable" || status.Attempts != 1 {
		t.Fatalf("outage status: %+v %v", status, err)
	}
	if count, err := outbox.Drain(ctx, 10); err != nil || count != 0 || remote.calls != 1 {
		t.Fatalf("backoff must suppress immediate retry: count=%d calls=%d err=%v", count, remote.calls, err)
	}
	remote.err = nil
	outbox.Now = func() time.Time { return time.Date(2026, 9, 24, 10, 2, 0, 0, time.UTC) }
	if count, err := outbox.Drain(ctx, 10); err != nil || count != 1 {
		t.Fatalf("retry: count=%d err=%v", count, err)
	}
	status, err = outbox.Get(ctx, msg.CandidateID)
	if err != nil || status.State != "submitted" || status.RemoteCommit != "abc123" || status.Attempts != 2 {
		t.Fatalf("submitted status: %+v %v", status, err)
	}
	if count, err := outbox.Drain(ctx, 10); err != nil || count != 0 || remote.calls != 2 {
		t.Fatalf("submitted candidate must not resend: count=%d calls=%d err=%v", count, remote.calls, err)
	}
	var effectCount int
	if err := data.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_effect_intents`).Scan(&effectCount); err != nil || effectCount != 0 {
		t.Fatalf("candidate retry must not create/replay task effects: count=%d err=%v", effectCount, err)
	}
}

func TestCandidateOutboxConflictAndCrashRecovery(t *testing.T) {
	ctx := context.Background()
	data, outbox, remote := testOutbox(t)
	msg := testCandidate()
	if _, err := outbox.Queue(ctx, msg); err != nil {
		t.Fatal(err)
	}
	task, check, approval := candidateTestFacts()
	task.Solution += " with a different export summary"
	approval.ContentDigest, _ = CandidateExportContentDigest(task)
	different, err := BuildKnowledgeCandidate(task, check, approval)
	if err != nil {
		t.Fatal(err)
	}
	if different.CandidateID != msg.CandidateID || different.PayloadDigest == msg.PayloadDigest {
		t.Fatal("fixture must preserve source identity while changing payload")
	}
	changedPayload, err := json.Marshal(different)
	if err != nil {
		t.Fatal(err)
	}
	changed := CandidateMessage{CandidateID: different.CandidateID, ProjectID: different.ProjectID,
		TaskID: task.HarnessTaskID, PayloadDigest: different.PayloadDigest,
		Payload: changedPayload, Approval: approval}
	if _, err := outbox.Queue(ctx, changed); !errors.Is(err, ErrCandidateConflict) {
		t.Fatalf("same ID with changed digest must fail closed: %v", err)
	}
	if _, err := data.DB.ExecContext(ctx, `UPDATE project_brain_candidate_outbox SET state='sending' WHERE candidate_id=?`, msg.CandidateID); err != nil {
		t.Fatal(err)
	}
	if count, err := outbox.Recover(ctx); err != nil || count != 1 {
		t.Fatalf("recover: count=%d err=%v", count, err)
	}
	if count, err := outbox.Drain(ctx, 10); err != nil || count != 1 || remote.calls != 1 {
		t.Fatalf("replay inert submission: count=%d calls=%d err=%v", count, remote.calls, err)
	}
}

func TestCandidateOutboxStopsOnRevokedExport(t *testing.T) {
	ctx := context.Background()
	_, outbox, remote := testOutbox(t)
	msg := testCandidate()
	if _, err := outbox.Queue(ctx, msg); err != nil {
		t.Fatal(err)
	}
	outbox.Authorize = func(context.Context, CandidateMessage) error { return ErrExportRevoked }
	if count, err := outbox.Drain(ctx, 1); err != nil || count != 0 || remote.calls != 0 {
		t.Fatalf("revoked export reached Pi: count=%d calls=%d err=%v", count, remote.calls, err)
	}
	status, err := outbox.Get(ctx, msg.CandidateID)
	if err != nil || status.State != "blocked" || status.LastError != "export_revoked" {
		t.Fatalf("revocation was not durably blocked: %+v %v", status, err)
	}
	outbox.Authorize = func(context.Context, CandidateMessage) error { return nil }
	msg.Approval.ApprovedAt = msg.Approval.ApprovedAt.Add(time.Hour)
	outbox.Now = func() time.Time { return msg.Approval.ApprovedAt.Add(time.Minute) }
	status, err = outbox.Queue(ctx, msg)
	if err != nil || status.State != "pending" {
		t.Fatalf("fresh owner approval did not requeue same payload: %+v %v", status, err)
	}
	if count, err := outbox.Drain(ctx, 1); err != nil || count != 1 || remote.calls != 1 {
		t.Fatalf("reauthorized submission: count=%d calls=%d err=%v", count, remote.calls, err)
	}
}
