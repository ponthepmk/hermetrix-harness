package projectbrain

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"hermetrix-harness/internal/capabilities"
)

type submitCatalog struct {
	fakeCatalog
	response string
	called   int
}

func (s *submitCatalog) Call(_ context.Context, id, revision string, args json.RawMessage) (capabilities.CallResult, capabilities.Entry, error) {
	if id != "submit_knowledge_candidate" || revision != "rev-1" {
		return capabilities.CallResult{}, capabilities.Entry{}, errors.New("wrong submission capability")
	}
	var request struct {
		Candidate map[string]any `json:"candidate"`
	}
	if json.Unmarshal(args, &request) != nil || request.Candidate["project_id"] != "hermetrix-harness" {
		return capabilities.CallResult{}, capabilities.Entry{}, errors.New("unscoped submission")
	}
	s.called++
	return capabilities.CallResult{Output: textResult(s.response)}, capabilities.Entry{}, nil
}

func submitFixture() (MCPSubmitter, *submitCatalog, json.RawMessage) {
	digest := "sha256:" + strings.Repeat("a", 64)
	response, _ := json.Marshal(SubmissionReceipt{Status: "submitted_unverified", CandidateID: "candidate-1",
		PayloadDigest: digest, Commit: "abc123"})
	catalog := &submitCatalog{response: string(response)}
	catalog.entries = []capabilities.Entry{{ID: "submit_knowledge_candidate", Name: "submit_knowledge_candidate",
		Source: capabilities.SourceMCP, SourceRef: "brain-write", Revision: "rev-1",
		Effect: "external_mutation", RequiresApproval: true, Readiness: capabilities.ReadinessReady,
		Metadata: map[string]any{"annotations_trusted": true}}}
	payload, _ := json.Marshal(map[string]string{"candidate_id": "candidate-1", "project_id": "hermetrix-harness",
		"payload_digest": digest})
	submitter := MCPSubmitter{Servers: fixedServers{{ID: "brain-write", Name: "Project Brain Submit", Enabled: true,
		Status: "ready", CredentialReady: true, TrustAnnotations: true}}, Catalog: catalog,
		ServerName: "Project Brain Submit", Project: "hermetrix-harness"}
	return submitter, catalog, payload
}

func TestMCPSubmitterUsesOnlyExactScopedWriteTool(t *testing.T) {
	submitter, catalog, payload := submitFixture()
	receipt, err := submitter.SubmitCandidate(context.Background(), payload)
	if err != nil || receipt.Status != "submitted_unverified" || catalog.called != 1 {
		t.Fatalf("scoped submission: receipt=%+v calls=%d err=%v", receipt, catalog.called, err)
	}
	submitter.Project = "other-client"
	if _, err := submitter.SubmitCandidate(context.Background(), payload); err == nil || catalog.called != 1 {
		t.Fatalf("foreign project escaped binding: calls=%d err=%v", catalog.called, err)
	}
	submitter.Project = "hermetrix-harness"
	catalog.entries[0].Effect = "read"
	if _, err := submitter.SubmitCandidate(context.Background(), payload); err == nil || catalog.called != 1 {
		t.Fatalf("misclassified read tool used for write: calls=%d err=%v", catalog.called, err)
	}
	for _, effect := range []string{"unknown", "destructive"} {
		catalog.entries[0].Effect = effect
		if _, err := submitter.SubmitCandidate(context.Background(), payload); err == nil || catalog.called != 1 {
			t.Fatalf("misclassified %s tool used for candidate: calls=%d err=%v", effect, catalog.called, err)
		}
	}
}

func TestMCPSubmitterRejectsInvalidPiReceipt(t *testing.T) {
	submitter, catalog, payload := submitFixture()
	catalog.response = `{"status":"submitted_unverified","candidate_id":"other","payload_digest":"sha256:` + strings.Repeat("a", 64) + `"}`
	if _, err := submitter.SubmitCandidate(context.Background(), payload); err == nil {
		t.Fatal("unbound Pi receipt accepted")
	}
	catalog.response = `IDEMPOTENCY_CONFLICT: candidate changed`
	if _, err := submitter.SubmitCandidate(context.Background(), payload); !errors.Is(err, ErrCandidateConflict) {
		t.Fatalf("Pi conflict should fail closed: %v", err)
	}
}
