package projectbrain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func candidateTestFacts() (CommittedTaskFacts, CommittedVerification, CandidateExportApproval) {
	created := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	task := CommittedTaskFacts{HarnessTaskID: "task-local-9", TaskRevision: 7,
		State: "completed", ProjectID: "hermetrix-harness", OriginNodeID: "node-windows-1",
		ProjectVisibility: "project_shared", ProjectExportPolicy: "explicit_selection",
		ProjectSharingRevision: 2, Visibility: "project_shared", ExportPolicy: "explicit_selection",
		SharingRevision: 3,
		AgentID:         "hermetrix-windows", PlatformTaskID: "platform-task-3",
		PlatformRunID: "platform-run-5", Title: "Repair migration reopen",
		Problem:     "The migration reopened with the wrong schema.",
		Solution:    "Commit the migration, reopen SQLite, and check its schema version.",
		CompletedAt: created.Add(3 * time.Minute)}
	content := []byte("migration reopen passed; schema 49")
	sum := sha256.Sum256(content)
	check := CommittedVerification{CheckID: "migration-reopen", SubjectRevision: "task-requirement-7-plan-3",
		Kind: "test", Outcome: "pass", ObservedAt: created.Add(2 * time.Minute),
		Evidence: []CommittedEvidence{{ID: "artifact-test-42", Type: "test_output", Content: content,
			ContentDigest: "sha256:" + hex.EncodeToString(sum[:]), CreatedAt: created.Add(time.Minute),
			HarnessRef: "artifact:artifact-test-42", Visibility: "project_shared",
			ExportPolicy: "explicit_selection", SharingRevision: 4}}}
	contentDigest, _ := CandidateExportContentDigest(task)
	approval := CandidateExportApproval{Actor: "owner", ProjectID: task.ProjectID,
		HarnessTaskID: task.HarnessTaskID, TaskRevision: task.TaskRevision,
		ValidationID: "validation-42", CheckID: check.CheckID, SubjectRevision: check.SubjectRevision,
		VerificationKind:       check.Kind,
		ProjectSharingRevision: task.ProjectSharingRevision, TaskSharingRevision: task.SharingRevision,
		ContentDigest: contentDigest, EvidenceDigests: map[string]string{
			check.Evidence[0].ID: check.Evidence[0].ContentDigest},
		EvidenceRevisions: map[string]int{check.Evidence[0].ID: check.Evidence[0].SharingRevision},
		ApprovedAt:        created.Add(4 * time.Minute)}
	return task, check, approval
}

func TestBuildKnowledgeCandidateMatchesPiRevisionTwoAndJCS(t *testing.T) {
	task, check, approval := candidateTestFacts()
	candidate, err := BuildKnowledgeCandidate(task, check, approval)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.ContractVersion != "agent-platform/v1" || candidate.SchemaRevision != 2 ||
		candidate.Type != "problem_solution" || candidate.ProjectID != task.ProjectID ||
		candidate.TaskID != task.PlatformTaskID || candidate.RunID != task.PlatformRunID ||
		candidate.Verification.Outcome != "pass" || candidate.Verification.Scope != check.SubjectRevision ||
		len(candidate.Verification.EvidenceRefs) != 1 || candidate.IdempotencyKey == "" {
		t.Fatalf("candidate lost required Pi facts: %+v", candidate)
	}
	ref := candidate.Verification.EvidenceRefs[0]
	if ref.OriginNodeID != task.OriginNodeID || ref.ContentDigest != check.Evidence[0].ContentDigest ||
		ref.Provenance == nil || ref.Provenance.HarnessRef != check.Evidence[0].HarnessRef {
		t.Fatalf("candidate lost immutable evidence provenance: %+v", ref)
	}
	unsigned := candidate
	unsigned.PayloadDigest = ""
	unsignedJSON, err := json.Marshal(unsigned)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest, err := candidateJCSDigest(unsignedJSON)
	if err != nil || candidate.PayloadDigest != wantDigest {
		t.Fatalf("non-self-referential JCS digest = %q, want %q: %v", candidate.PayloadDigest, wantDigest, err)
	}
	encoded, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if _, exists := fields["harness_task_id"]; exists || strings.Contains(string(encoded), "migration reopen passed; schema 49") ||
		strings.Contains(string(encoded), "C:\\") {
		t.Fatalf("candidate leaked local identity, evidence bytes, or absolute path: %s", encoded)
	}
	// A synthetic wire fixture can be fed to Pi's actual revision-2 validator.
	t.Logf("PI_CANDIDATE_JSON=%s", encoded)
}

func TestBuildKnowledgeCandidateStableAcrossRetryAndEvidenceOrder(t *testing.T) {
	task, check, approval := candidateTestFacts()
	otherContent := []byte("independent review approved")
	otherSum := sha256.Sum256(otherContent)
	check.Evidence = append(check.Evidence, CommittedEvidence{ID: "artifact-review-44", Type: "artifact",
		Content: otherContent, ContentDigest: "sha256:" + hex.EncodeToString(otherSum[:]),
		CreatedAt: check.ObservedAt, HarnessRef: "artifact:artifact-review-44",
		Visibility: "project_shared", ExportPolicy: "explicit_selection", SharingRevision: 2})
	approval.EvidenceDigests["artifact-review-44"] = "sha256:" + hex.EncodeToString(otherSum[:])
	approval.EvidenceRevisions["artifact-review-44"] = 2
	first, err := BuildKnowledgeCandidate(task, check, approval)
	if err != nil {
		t.Fatal(err)
	}
	check.Evidence[0], check.Evidence[1] = check.Evidence[1], check.Evidence[0]
	second, err := BuildKnowledgeCandidate(task, check, approval)
	if err != nil || first.CandidateID != second.CandidateID ||
		first.IdempotencyKey != second.IdempotencyKey || first.PayloadDigest != second.PayloadDigest {
		t.Fatalf("retry/order changed candidate identity or payload: first=%+v second=%+v err=%v", first, second, err)
	}
	task.Solution += " Include a fresh reopen check."
	approval.ContentDigest, _ = CandidateExportContentDigest(task)
	changed, err := BuildKnowledgeCandidate(task, check, approval)
	if err != nil || changed.CandidateID != first.CandidateID || changed.PayloadDigest == first.PayloadDigest {
		t.Fatalf("same source with changed prose must create Pi conflict: changed=%+v err=%v", changed, err)
	}
}

func TestBuildKnowledgeCandidateRejectsUnverifiedOrMutableFacts(t *testing.T) {
	cases := map[string]func(*CommittedTaskFacts, *CommittedVerification){
		"unfinished task":          func(task *CommittedTaskFacts, _ *CommittedVerification) { task.State = "verifying" },
		"missing local task ID":    func(task *CommittedTaskFacts, _ *CommittedVerification) { task.HarnessTaskID = "" },
		"missing project":          func(task *CommittedTaskFacts, _ *CommittedVerification) { task.ProjectID = "" },
		"absolute path identity":   func(task *CommittedTaskFacts, _ *CommittedVerification) { task.HarnessTaskID = `C:\\private\\task` },
		"no solution":              func(task *CommittedTaskFacts, _ *CommittedVerification) { task.Solution = "" },
		"new completion timestamp": func(task *CommittedTaskFacts, _ *CommittedVerification) { task.CompletedAt = time.Time{} },
		"failed check":             func(_ *CommittedTaskFacts, check *CommittedVerification) { check.Outcome = "fail" },
		"blocked check":            func(_ *CommittedTaskFacts, check *CommittedVerification) { check.Outcome = "blocked" },
		"no check identity":        func(_ *CommittedTaskFacts, check *CommittedVerification) { check.CheckID = "" },
		"no subject revision":      func(_ *CommittedTaskFacts, check *CommittedVerification) { check.SubjectRevision = "" },
		"no evidence":              func(_ *CommittedTaskFacts, check *CommittedVerification) { check.Evidence = nil },
		"missing bytes":            func(_ *CommittedTaskFacts, check *CommittedVerification) { check.Evidence[0].Content = nil },
		"wrong digest": func(_ *CommittedTaskFacts, check *CommittedVerification) {
			check.Evidence[0].ContentDigest = "sha256:" + strings.Repeat("0", 64)
		},
		"mutable digest":        func(_ *CommittedTaskFacts, check *CommittedVerification) { check.Evidence[0].ContentDigest = "latest" },
		"unknown evidence type": func(_ *CommittedTaskFacts, check *CommittedVerification) { check.Evidence[0].Type = "file_path" },
		"absolute evidence ref": func(_ *CommittedTaskFacts, check *CommittedVerification) {
			check.Evidence[0].HarnessRef = `C:\\private\\test.log`
		},
		"duplicate evidence": func(_ *CommittedTaskFacts, check *CommittedVerification) {
			check.Evidence = append(check.Evidence, check.Evidence[0])
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			task, check, approval := candidateTestFacts()
			mutate(&task, &check)
			if candidate, err := BuildKnowledgeCandidate(task, check, approval); err == nil {
				t.Fatalf("unsafe candidate accepted: %+v", candidate)
			}
		})
	}
}

func TestBuildKnowledgeCandidateRequiresExplicitExportSelection(t *testing.T) {
	cases := map[string]func(*CommittedTaskFacts, *CommittedVerification, *CandidateExportApproval){
		"default private project": func(task *CommittedTaskFacts, _ *CommittedVerification, _ *CandidateExportApproval) {
			task.ProjectVisibility, task.ProjectExportPolicy = "private", "deny"
		},
		"default private task": func(task *CommittedTaskFacts, _ *CommittedVerification, _ *CandidateExportApproval) {
			task.Visibility, task.ExportPolicy = "private", "deny"
		},
		"default private evidence": func(_ *CommittedTaskFacts, check *CommittedVerification, _ *CandidateExportApproval) {
			check.Evidence[0].Visibility, check.Evidence[0].ExportPolicy = "private", "deny"
		},
		"no approval": func(_ *CommittedTaskFacts, _ *CommittedVerification, approval *CandidateExportApproval) {
			*approval = CandidateExportApproval{}
		},
		"wrong target project": func(_ *CommittedTaskFacts, _ *CommittedVerification, approval *CandidateExportApproval) {
			approval.ProjectID = "other-client"
		},
		"stale task revision": func(_ *CommittedTaskFacts, _ *CommittedVerification, approval *CandidateExportApproval) {
			approval.TaskRevision--
		},
		"different validation": func(_ *CommittedTaskFacts, _ *CommittedVerification, approval *CandidateExportApproval) {
			approval.ValidationID = ""
		},
		"different check": func(_ *CommittedTaskFacts, _ *CommittedVerification, approval *CandidateExportApproval) {
			approval.CheckID = "different-check"
		},
		"different verification kind": func(_ *CommittedTaskFacts, _ *CommittedVerification, approval *CandidateExportApproval) {
			approval.VerificationKind = "review"
		},
		"stale subject": func(_ *CommittedTaskFacts, _ *CommittedVerification, approval *CandidateExportApproval) {
			approval.SubjectRevision = "requirement:old"
		},
		"unselected evidence": func(_ *CommittedTaskFacts, _ *CommittedVerification, approval *CandidateExportApproval) {
			approval.EvidenceDigests = nil
		},
		"revoked project revision": func(task *CommittedTaskFacts, _ *CommittedVerification, _ *CandidateExportApproval) {
			task.ProjectSharingRevision++
		},
		"revoked task revision": func(task *CommittedTaskFacts, _ *CommittedVerification, _ *CandidateExportApproval) {
			task.SharingRevision++
		},
		"revoked evidence revision": func(_ *CommittedTaskFacts, check *CommittedVerification, _ *CandidateExportApproval) {
			check.Evidence[0].SharingRevision++
		},
		"text changed after approval": func(task *CommittedTaskFacts, _ *CommittedVerification, _ *CandidateExportApproval) {
			task.Problem += " Added a private detail."
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			task, check, approval := candidateTestFacts()
			mutate(&task, &check, &approval)
			if candidate, err := BuildKnowledgeCandidate(task, check, approval); err == nil {
				t.Fatalf("unapproved export was built: %+v", candidate)
			}
		})
	}
}

func TestCandidateJCSMatchesPiRFC8785UnicodeFixture(t *testing.T) {
	// Expected digest was independently calculated with Pi's rfc8785.dumps.
	raw := []byte(`{"\ue000":1,"😀":2,"z":"<\u2028","é":3}`)
	digest, err := candidateJCSDigest(raw)
	if err != nil || digest != "sha256:57e14118abc3f5a450032c07ce379f380c62f318d6eae0bb5cb43b95d9f9dc93" {
		t.Fatalf("candidate JCS diverged from Pi: digest=%q err=%v", digest, err)
	}
	if _, err := candidateJCSDigest([]byte(`{"fraction":1.5}`)); err == nil {
		t.Fatal("candidate JCS accepted a number outside the closed candidate schema")
	}
}
