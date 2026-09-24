package projectbrain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"hermetrix-harness/internal/capabilities"
	"hermetrix-harness/internal/mcp"
)

// MCPSubmitter delivers only an explicitly staged KnowledgeCandidate through a
// separately configured, project-scoped Pi write credential. It never calls a
// curation tool and never owns an MCP token value.
type MCPSubmitter struct {
	Servers    ServerLister
	Catalog    Catalog
	ServerName string
	Project    string
}

func (s MCPSubmitter) SubmitCandidate(ctx context.Context, payload json.RawMessage) (SubmissionReceipt, error) {
	if s.Servers == nil || s.Catalog == nil || strings.TrimSpace(s.ServerName) == "" || strings.TrimSpace(s.Project) == "" {
		return SubmissionReceipt{}, errors.New("Project Brain candidate submission is not configured")
	}
	var candidate struct {
		CandidateID   string `json:"candidate_id"`
		ProjectID     string `json:"project_id"`
		PayloadDigest string `json:"payload_digest"`
	}
	if json.Unmarshal(payload, &candidate) != nil || candidate.CandidateID == "" ||
		candidate.ProjectID != s.Project || !versionPattern.MatchString(candidate.PayloadDigest) {
		return SubmissionReceipt{}, errors.New("candidate does not match the configured Project Brain project")
	}
	servers, err := s.Servers.List(ctx)
	if err != nil {
		return SubmissionReceipt{}, fmt.Errorf("find Project Brain candidate server: %w", err)
	}
	var server *mcp.Server
	for i := range servers {
		if servers[i].Name != s.ServerName {
			continue
		}
		if server != nil {
			return SubmissionReceipt{}, errors.New("Project Brain candidate server name is ambiguous")
		}
		server = &servers[i]
	}
	if server == nil || !server.Enabled || server.Status != "ready" || !server.CredentialReady || !server.TrustAnnotations {
		return SubmissionReceipt{}, errors.New("Project Brain candidate server is not ready")
	}
	var tool capabilities.Entry
	for _, match := range s.Catalog.Search("submit_knowledge_candidate", capabilities.SourceMCP, 100) {
		if match.SourceRef != server.ID || match.Name != "submit_knowledge_candidate" {
			continue
		}
		tool, err = s.Catalog.Describe(match.ID)
		if err != nil {
			return SubmissionReceipt{}, err
		}
		break
	}
	if tool.ID == "" || tool.Readiness != capabilities.ReadinessReady || tool.SourceRef != server.ID ||
		tool.Effect != "external_mutation" || !tool.RequiresApproval || tool.Metadata["annotations_trusted"] != true {
		return SubmissionReceipt{}, errors.New("Project Brain candidate write tool is not discovered with expected risk")
	}
	arguments, err := json.Marshal(struct {
		Candidate json.RawMessage `json:"candidate"`
	}{Candidate: payload})
	if err != nil {
		return SubmissionReceipt{}, err
	}
	result, _, err := s.Catalog.Call(ctx, tool.ID, tool.Revision, arguments)
	if err != nil {
		if strings.Contains(err.Error(), "IDEMPOTENCY_CONFLICT") {
			return SubmissionReceipt{}, ErrCandidateConflict
		}
		return SubmissionReceipt{}, errors.New("Project Brain candidate transport unavailable")
	}
	if strings.Contains(result.Output, "IDEMPOTENCY_CONFLICT") {
		return SubmissionReceipt{}, ErrCandidateConflict
	}
	var receipt SubmissionReceipt
	if err := decodeTextResult(result.Output, &receipt); err != nil {
		return SubmissionReceipt{}, errors.New("Project Brain candidate response is invalid")
	}
	if receipt.CandidateID != candidate.CandidateID || receipt.PayloadDigest != candidate.PayloadDigest ||
		(receipt.Status != "submitted_unverified" && receipt.Status != "already_submitted") {
		return SubmissionReceipt{}, errors.New("Project Brain candidate response does not match submission")
	}
	return receipt, nil
}
