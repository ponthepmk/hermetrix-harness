package projectbrain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const candidateSchemaRevision = 2
const candidateContractVersion = "agent-platform/v1"

var candidateDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// CommittedTaskFacts must be populated from a committed, completed local task.
// Platform IDs are optional cross-references and never stand in for Harness IDs.
// The builder does not read a database or assert that a caller's facts are true.
type CommittedTaskFacts struct {
	HarnessTaskID string
	TaskRevision  int
	State         string
	ProjectID     string
	// The existing sharing policy defaults to private/deny. These facts must
	// be read from the same committed source revision as the task projection.
	ProjectVisibility      string
	ProjectExportPolicy    string
	ProjectSharingRevision int
	Visibility             string
	ExportPolicy           string
	SharingRevision        int
	OriginNodeID           string
	AgentID                string
	PlatformTaskID         string
	PlatformRunID          string
	Title                  string
	Problem                string
	Solution               string
	CompletedAt            time.Time
}

// CommittedEvidence carries the exact immutable bytes that the caller read
// from local CAS. Only their digest and opaque identity leave this builder.
type CommittedEvidence struct {
	ID              string
	Type            string
	Content         []byte
	ContentDigest   string
	CreatedAt       time.Time
	HarnessRef      string
	Visibility      string
	ExportPolicy    string
	SharingRevision int
}

// CandidateExportApproval is an explicit selection of exactly this text and
// these evidence digests for sharing with this Pi project. It is not an
// authorization credential: a runtime adapter must verify a persisted owner
// approval and its revision before calling this pure builder or sending bytes.
type CandidateExportApproval struct {
	Actor                  string            `json:"actor"`
	ProjectID              string            `json:"project_id"`
	HarnessTaskID          string            `json:"harness_task_id"`
	TaskRevision           int               `json:"task_revision"`
	ValidationID           string            `json:"validation_id"`
	CheckID                string            `json:"check_id"`
	SubjectRevision        string            `json:"subject_revision"`
	VerificationKind       string            `json:"verification_kind"`
	ContentDigest          string            `json:"content_digest"`
	EvidenceDigests        map[string]string `json:"evidence_digests"`
	ProjectSharingRevision int               `json:"project_sharing_revision"`
	TaskSharingRevision    int               `json:"task_sharing_revision"`
	EvidenceRevisions      map[string]int    `json:"evidence_revisions"`
	ApprovedAt             time.Time         `json:"approved_at"`
}

// CommittedVerification identifies one passed check of the active subject.
// A future adapter must re-read these facts from the durable validation and
// evidence stores; a model response is not a verification source.
type CommittedVerification struct {
	CheckID         string
	SubjectRevision string
	Kind            string
	Outcome         string
	ObservedAt      time.Time
	Evidence        []CommittedEvidence
}

// KnowledgeCandidate is Pi's schema revision 2 wire shape. Submission remains
// an inert claim; this package has no transport or promotion capability.
type KnowledgeCandidate struct {
	ContractVersion string                `json:"contract_version"`
	SchemaRevision  int                   `json:"schema_revision"`
	CandidateID     string                `json:"candidate_id"`
	ProjectID       string                `json:"project_id"`
	TaskID          string                `json:"task_id,omitempty"`
	RunID           string                `json:"run_id,omitempty"`
	Type            string                `json:"type"`
	Title           string                `json:"title"`
	Problem         string                `json:"problem"`
	Solution        string                `json:"solution"`
	Tests           []string              `json:"tests"`
	Verification    CandidateVerification `json:"verification"`
	Provenance      CandidateProvenance   `json:"provenance"`
	CreatedAt       string                `json:"created_at"`
	IdempotencyKey  string                `json:"idempotency_key"`
	PayloadDigest   string                `json:"payload_digest,omitempty"`
}

type CandidateVerification struct {
	Kind         string                 `json:"kind"`
	Outcome      string                 `json:"outcome"`
	Scope        string                 `json:"scope"`
	ObservedAt   string                 `json:"observed_at"`
	EvidenceRefs []CandidateEvidenceRef `json:"evidence_refs"`
}

type CandidateProvenance struct {
	AgentID       string `json:"agent_id"`
	PlatformRunID string `json:"platform_run_id,omitempty"`
}

type CandidateEvidenceRef struct {
	EvidenceID    string                       `json:"evidence_id"`
	Type          string                       `json:"type"`
	OriginNodeID  string                       `json:"origin_node_id"`
	ContentDigest string                       `json:"content_digest"`
	CreatedAt     string                       `json:"created_at"`
	Provenance    *CandidateEvidenceProvenance `json:"provenance,omitempty"`
}

type CandidateEvidenceProvenance struct {
	AgentID    string `json:"agent_id"`
	HarnessRef string `json:"harness_ref,omitempty"`
}

var evidenceTypes = map[string]bool{
	"log": true, "diff": true, "test_output": true, "screenshot": true,
	"artifact": true, "cas_object": true, "job": true, "validation": true, "event": true,
}

var verificationKinds = map[string]bool{
	"test": true, "build": true, "manual": true, "review": true, "reproduction": true,
}

// BuildKnowledgeCandidate deterministically serializes committed facts into a
// Pi candidate. Same source identity and payload are idempotent; changing the
// prose without changing the source identity yields the same candidate ID and
// a different payload digest, which Pi rejects as an idempotency conflict.
func BuildKnowledgeCandidate(task CommittedTaskFacts, check CommittedVerification,
	approval CandidateExportApproval) (KnowledgeCandidate, error) {
	if task.State != "completed" || task.TaskRevision < 1 || task.CompletedAt.IsZero() {
		return KnowledgeCandidate{}, errors.New("candidate requires a committed completed task revision")
	}
	if task.ProjectVisibility != "project_shared" || task.ProjectExportPolicy != "explicit_selection" ||
		task.Visibility != "project_shared" || task.ExportPolicy != "explicit_selection" ||
		task.ProjectSharingRevision < 1 || task.SharingRevision < 1 {
		return KnowledgeCandidate{}, errors.New("candidate source project and task are not eligible for explicit export")
	}
	for name, value := range map[string]string{
		"harness task ID": task.HarnessTaskID, "Pi project ID": task.ProjectID,
		"origin node ID": task.OriginNodeID, "agent ID": task.AgentID,
		"check ID": check.CheckID, "subject revision": check.SubjectRevision,
	} {
		if !candidateAtom(value) {
			return KnowledgeCandidate{}, fmt.Errorf("candidate %s is missing or is not an opaque identity", name)
		}
	}
	if (task.PlatformTaskID != "" && !candidateAtom(task.PlatformTaskID)) ||
		(task.PlatformRunID != "" && !candidateAtom(task.PlatformRunID)) {
		return KnowledgeCandidate{}, errors.New("candidate Platform cross-reference is invalid")
	}
	if !candidateText(task.Title) || strings.ContainsAny(task.Title, "\r\n") ||
		!candidateText(task.Problem) || !candidateText(task.Solution) {
		return KnowledgeCandidate{}, errors.New("candidate needs a nonblank title, problem, and solution")
	}
	contentDigest, err := CandidateExportContentDigest(task)
	if err != nil {
		return KnowledgeCandidate{}, err
	}
	if !candidateAtom(approval.Actor) || approval.ProjectID != task.ProjectID ||
		approval.HarnessTaskID != task.HarnessTaskID || approval.TaskRevision != task.TaskRevision ||
		!candidateAtom(approval.ValidationID) || approval.CheckID != check.CheckID ||
		approval.SubjectRevision != check.SubjectRevision ||
		approval.VerificationKind != check.Kind ||
		approval.ProjectSharingRevision != task.ProjectSharingRevision ||
		approval.TaskSharingRevision != task.SharingRevision ||
		approval.ContentDigest != contentDigest || approval.ApprovedAt.IsZero() ||
		len(approval.EvidenceDigests) != len(check.Evidence) ||
		len(approval.EvidenceRevisions) != len(check.Evidence) {
		return KnowledgeCandidate{}, errors.New("candidate lacks a matching explicit export selection")
	}
	if check.Outcome != "pass" || !verificationKinds[check.Kind] || check.ObservedAt.IsZero() ||
		len(check.Evidence) == 0 {
		return KnowledgeCandidate{}, errors.New("candidate requires a passed check with immutable evidence")
	}
	refs := make([]CandidateEvidenceRef, 0, len(check.Evidence))
	seen := map[string]bool{}
	for _, evidence := range check.Evidence {
		if !candidateAtom(evidence.ID) || !evidenceTypes[evidence.Type] ||
			!candidateDigestPattern.MatchString(evidence.ContentDigest) || evidence.CreatedAt.IsZero() ||
			len(evidence.Content) == 0 || seen[evidence.ID] ||
			evidence.Visibility != "project_shared" || evidence.ExportPolicy != "explicit_selection" ||
			evidence.SharingRevision < 1 || approval.EvidenceRevisions[evidence.ID] != evidence.SharingRevision ||
			approval.EvidenceDigests[evidence.ID] != evidence.ContentDigest ||
			(evidence.HarnessRef != "" && !candidateHarnessRef(evidence.HarnessRef)) {
			return KnowledgeCandidate{}, errors.New("candidate evidence identity, type, content, or provenance is invalid")
		}
		seen[evidence.ID] = true
		sum := sha256.Sum256(evidence.Content)
		if evidence.ContentDigest != "sha256:"+hex.EncodeToString(sum[:]) {
			return KnowledgeCandidate{}, fmt.Errorf("candidate evidence %s does not match immutable content digest", evidence.ID)
		}
		ref := CandidateEvidenceRef{EvidenceID: evidence.ID, Type: evidence.Type,
			OriginNodeID: task.OriginNodeID, ContentDigest: evidence.ContentDigest,
			CreatedAt:  evidence.CreatedAt.UTC().Format(time.RFC3339Nano),
			Provenance: &CandidateEvidenceProvenance{AgentID: task.AgentID, HarnessRef: evidence.HarnessRef}}
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].EvidenceID < refs[j].EvidenceID })
	// The identity excludes generated prose and the candidate creation time;
	// committed evidence timestamps remain part of the source identity. A
	// same-revision retry cannot mint a different Pi draft by changing wording.
	identityFacts := struct {
		ProjectID, NodeID, AgentID, HarnessTaskID, PlatformTaskID, PlatformRunID string
		TaskRevision                                                             int
		SubjectRevision, CheckID, ValidationID                                   string
		EvidenceRefs                                                             []CandidateEvidenceRef
	}{task.ProjectID, task.OriginNodeID, task.AgentID, task.HarnessTaskID,
		task.PlatformTaskID, task.PlatformRunID, task.TaskRevision,
		check.SubjectRevision, check.CheckID, approval.ValidationID, refs}
	identityRaw, err := json.Marshal(identityFacts)
	if err != nil {
		return KnowledgeCandidate{}, err
	}
	identityDigest, err := candidateJCSDigest(identityRaw)
	if err != nil {
		return KnowledgeCandidate{}, fmt.Errorf("canonicalize candidate source identity: %w", err)
	}
	identityHex := strings.TrimPrefix(identityDigest, "sha256:")
	candidate := KnowledgeCandidate{
		ContractVersion: candidateContractVersion, SchemaRevision: candidateSchemaRevision,
		CandidateID: "hermetrix-knowledge-" + identityHex, ProjectID: task.ProjectID,
		TaskID: task.PlatformTaskID, RunID: task.PlatformRunID,
		Type: "problem_solution", Title: task.Title, Problem: task.Problem, Solution: task.Solution,
		Tests: []string{check.CheckID}, Verification: CandidateVerification{
			Kind: check.Kind, Outcome: "pass", Scope: check.SubjectRevision,
			ObservedAt: check.ObservedAt.UTC().Format(time.RFC3339Nano), EvidenceRefs: refs},
		Provenance:     CandidateProvenance{AgentID: task.AgentID, PlatformRunID: task.PlatformRunID},
		CreatedAt:      task.CompletedAt.UTC().Format(time.RFC3339Nano),
		IdempotencyKey: "hermetrix-knowledge-" + identityHex,
	}
	unsigned, err := json.Marshal(candidate)
	if err != nil {
		return KnowledgeCandidate{}, err
	}
	candidate.PayloadDigest, err = candidateJCSDigest(unsigned)
	if err != nil {
		return KnowledgeCandidate{}, fmt.Errorf("canonicalize candidate payload: %w", err)
	}
	encoded, err := json.Marshal(candidate)
	if err != nil || len(encoded) > 20*1024 {
		return KnowledgeCandidate{}, errors.New("candidate exceeds bounded Pi payload size")
	}
	return candidate, nil
}

// CandidateExportContentDigest binds a human-reviewable export selection to
// the exact project/task revision and prose being sent. It does not authorize
// that selection; the persisted sharing approval remains the authority.
func CandidateExportContentDigest(task CommittedTaskFacts) (string, error) {
	if !candidateText(task.Title) || !candidateText(task.Problem) || !candidateText(task.Solution) ||
		!candidateAtom(task.ProjectID) || !candidateAtom(task.HarnessTaskID) || task.TaskRevision < 1 {
		return "", errors.New("invalid candidate export content")
	}
	raw, err := json.Marshal(struct {
		ProjectID     string `json:"project_id"`
		HarnessTaskID string `json:"harness_task_id"`
		TaskRevision  int    `json:"task_revision"`
		Title         string `json:"title"`
		Problem       string `json:"problem"`
		Solution      string `json:"solution"`
	}{task.ProjectID, task.HarnessTaskID, task.TaskRevision, task.Title, task.Problem, task.Solution})
	if err != nil {
		return "", err
	}
	return candidateJCSDigest(raw)
}

func candidateAtom(value string) bool {
	if value == "" || len(value) > 256 || value != strings.TrimSpace(value) || !utf8.ValidString(value) ||
		strings.ContainsAny(value, "/\\\x00\r\n\t") || strings.Contains(value, "..") {
		return false
	}
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return false
		}
	}
	return true
}

func candidateHarnessRef(value string) bool {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 || !candidateAtom(parts[1]) {
		return false
	}
	switch parts[0] {
	case "artifact", "cas", "event", "job", "validation", "effect":
		return true
	default:
		return false
	}
}

func candidateText(value string) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != ""
}
