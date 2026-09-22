package agentplatform

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

var criterionID = regexp.MustCompile(`^AC-[A-Za-z0-9]+$`)

var (
	ErrValidation       = errors.New("agent platform validation failed")
	ErrWriteUnsupported = errors.New("H1 rejects write authority")
	ErrDigestConflict   = errors.New("agent platform digest conflict")
	ErrUnsupported      = errors.New("agent platform operation is outside H1")
)

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func SignClaim(claim AssignmentAuthorizationClaim, privateKey ed25519.PrivateKey) (AssignmentAuthorizationClaim, error) {
	claim.Integrity.Value = ""
	input, err := claimSignatureInput(claim)
	if err != nil {
		return claim, err
	}
	claim.Integrity.Value = base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, input))
	return claim, nil
}

func claimSignatureInput(claim AssignmentAuthorizationClaim) ([]byte, error) {
	claim.Integrity.Value = ""
	raw, err := json.Marshal(claim)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err = decoder.Decode(&object); err != nil {
		return nil, err
	}
	integrity := object["integrity"].(map[string]any)
	delete(integrity, "value")
	trimmed, _ := json.Marshal(object)
	return CanonicalizeJSON(trimmed)
}

func SignEnvelope(messageType string, revision int, senderNodeID, idempotencyKey string, payload any, keyID string, privateKey ed25519.PrivateKey) ([]byte, ManagedEnvelope, error) {
	payloadRaw, err := json.Marshal(payload)
	if err != nil {
		return nil, ManagedEnvelope{}, err
	}
	digest, canonicalPayload, err := DigestJSON(payloadRaw)
	if err != nil {
		return nil, ManagedEnvelope{}, err
	}
	envelope := ManagedEnvelope{IdempotencyKey: idempotencyKey, MessageType: messageType,
		ContractVersion: ContractVersion, SchemaRevision: revision, SenderNodeID: senderNodeID,
		Payload: canonicalPayload, PayloadDigest: digest, Signature: Signature{Alg: "ed25519", KeyID: keyID}}
	input, err := envelopeSignatureInput(envelope)
	if err != nil {
		return nil, ManagedEnvelope{}, err
	}
	envelope.Signature.Value = base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, input))
	encoded, err := json.Marshal(envelope)
	return encoded, envelope, err
}

func envelopeSignatureInput(envelope ManagedEnvelope) ([]byte, error) {
	unsigned := struct {
		IdempotencyKey  string          `json:"idempotency_key"`
		MessageType     string          `json:"message_type"`
		ContractVersion string          `json:"contract_version"`
		SchemaRevision  int             `json:"schema_revision"`
		SenderNodeID    string          `json:"sender_node_id"`
		Payload         json.RawMessage `json:"payload"`
		PayloadDigest   string          `json:"payload_digest"`
	}{envelope.IdempotencyKey, envelope.MessageType, envelope.ContractVersion, envelope.SchemaRevision,
		envelope.SenderNodeID, envelope.Payload, envelope.PayloadDigest}
	raw, err := json.Marshal(unsigned)
	if err != nil {
		return nil, err
	}
	return CanonicalizeJSON(raw)
}

func VerifyEnvelope(raw []byte, trust Trust, messageType string, revision int) (ManagedEnvelope, error) {
	if len(raw) == 0 || len(raw) > 256<<10 {
		return ManagedEnvelope{}, fmt.Errorf("%w: envelope size", ErrValidation)
	}
	if _, err := CanonicalizeJSON(raw); err != nil {
		return ManagedEnvelope{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	if err := validateSchema("envelope", raw); err != nil {
		return ManagedEnvelope{}, fmt.Errorf("%w: envelope schema: %v", ErrValidation, err)
	}
	var envelope ManagedEnvelope
	if err := decodeStrict(raw, &envelope); err != nil {
		return ManagedEnvelope{}, fmt.Errorf("%w: envelope: %v", ErrValidation, err)
	}
	if envelope.ContractVersion != ContractVersion || envelope.SchemaRevision != revision || envelope.MessageType != messageType ||
		envelope.SenderNodeID != trust.NodeID || strings.TrimSpace(envelope.IdempotencyKey) == "" {
		return ManagedEnvelope{}, fmt.Errorf("%w: envelope identity or revision", ErrValidation)
	}
	digest, canonicalPayload, err := DigestJSON(envelope.Payload)
	if err != nil || digest != envelope.PayloadDigest || !bytes.Equal(canonicalPayload, envelope.Payload) {
		return ManagedEnvelope{}, fmt.Errorf("%w: payload digest or canonical bytes", ErrValidation)
	}
	if envelope.Signature.Alg != "ed25519" || envelope.Signature.KeyID != trust.KeyID || len(trust.PublicKey) != ed25519.PublicKeySize {
		return ManagedEnvelope{}, fmt.Errorf("%w: signature profile", ErrValidation)
	}
	signature, err := base64.RawURLEncoding.DecodeString(envelope.Signature.Value)
	if err != nil {
		return ManagedEnvelope{}, fmt.Errorf("%w: signature encoding", ErrValidation)
	}
	input, err := envelopeSignatureInput(envelope)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(trust.PublicKey), input, signature) {
		return ManagedEnvelope{}, fmt.Errorf("%w: invalid signature", ErrValidation)
	}
	return envelope, nil
}

func ValidateAssignment(raw []byte, trust Trust) (ManagedEnvelope, TaskAssignment, error) {
	envelope, err := VerifyEnvelope(raw, trust, "TaskAssignment", TaskAssignmentRevision)
	if err != nil {
		return ManagedEnvelope{}, TaskAssignment{}, err
	}
	if err = validateSchema("task_assignment", envelope.Payload); err != nil {
		return ManagedEnvelope{}, TaskAssignment{}, fmt.Errorf("%w: assignment schema: %v", ErrValidation, err)
	}
	var assignment TaskAssignment
	if err = decodeStrict(envelope.Payload, &assignment); err != nil {
		return ManagedEnvelope{}, TaskAssignment{}, fmt.Errorf("%w: assignment: %v", ErrValidation, err)
	}
	if assignment.ContractVersion != ContractVersion || assignment.SchemaRevision != TaskAssignmentRevision ||
		assignment.AssignmentRevision < 1 || assignment.AssignmentID == "" || assignment.PlatformRunID == "" || assignment.TaskID == "" ||
		assignment.Title == "" || assignment.Goal == "" || assignment.OriginalRequest == "" {
		return ManagedEnvelope{}, TaskAssignment{}, fmt.Errorf("%w: assignment required fields", ErrValidation)
	}
	if assignment.NodeID != trust.NodeID || assignment.AgentID != trust.AgentID || assignment.RepositoryID == "" ||
		assignment.AccessScope.RepositoryID != assignment.RepositoryID || !sameOptional(assignment.WorktreeID, assignment.AccessScope.WorktreeID) {
		return ManagedEnvelope{}, TaskAssignment{}, fmt.Errorf("%w: assignment target mismatch", ErrValidation)
	}
	if assignment.AccessScope.Access == "write" {
		return ManagedEnvelope{}, TaskAssignment{}, ErrWriteUnsupported
	}
	if assignment.AccessScope.Access != "none" && assignment.AccessScope.Access != "read" {
		return ManagedEnvelope{}, TaskAssignment{}, fmt.Errorf("%w: invalid access", ErrValidation)
	}
	if assignment.AuthorityScopeID != nil || assignment.AuthorityGeneration != nil {
		return ManagedEnvelope{}, TaskAssignment{}, fmt.Errorf("%w: unexpected write authority", ErrValidation)
	}
	if assignment.KnowledgeContextRef != nil {
		return ManagedEnvelope{}, TaskAssignment{}, fmt.Errorf("%w: ContextPack is not implemented by H1", ErrUnsupported)
	}
	if len(assignment.AcceptanceCriteria) == 0 || len(assignment.AcceptanceCriteria) > 50 {
		return ManagedEnvelope{}, TaskAssignment{}, fmt.Errorf("%w: acceptance criteria count", ErrValidation)
	}
	seen := map[string]bool{}
	for _, item := range assignment.AcceptanceCriteria {
		if !criterionID.MatchString(item.ID) || strings.TrimSpace(item.Description) == "" || seen[item.ID] {
			return ManagedEnvelope{}, TaskAssignment{}, fmt.Errorf("%w: acceptance criterion", ErrValidation)
		}
		seen[item.ID] = true
	}
	if err := validateClaim(assignment, trust); err != nil {
		return ManagedEnvelope{}, TaskAssignment{}, err
	}
	if _, err := parseContractTime(assignment.IssuedAt); err != nil {
		return ManagedEnvelope{}, TaskAssignment{}, fmt.Errorf("%w: issued_at", ErrValidation)
	}
	return envelope, assignment, nil
}

func ValidateRunUpdate(raw []byte, trust Trust) (ManagedEnvelope, RunUpdate, error) {
	envelope, err := VerifyEnvelope(raw, trust, "RunUpdate", RunUpdateRevision)
	if err != nil {
		return ManagedEnvelope{}, RunUpdate{}, err
	}
	if err = validateSchema("run_update", envelope.Payload); err != nil {
		return ManagedEnvelope{}, RunUpdate{}, fmt.Errorf("%w: run update schema: %v", ErrValidation, err)
	}
	var update RunUpdate
	if err = decodeStrict(envelope.Payload, &update); err != nil {
		return ManagedEnvelope{}, RunUpdate{}, fmt.Errorf("%w: run update: %v", ErrValidation, err)
	}
	if update.PlatformRunID == "" || update.Sequence < 1 || update.HarnessRunRef != nil ||
		update.AuthorityScopeID != nil || update.AuthorityGeneration != nil ||
		update.ProjectionRef.Type != "artifact" || update.ProjectionRef.OriginNodeID != trust.NodeID ||
		!strings.HasPrefix(update.ProjectionRef.ContentDigest, "sha256:") {
		return ManagedEnvelope{}, RunUpdate{}, fmt.Errorf("%w: run update read-only binding", ErrValidation)
	}
	if _, err = parseContractTime(update.CreatedAt); err != nil {
		return ManagedEnvelope{}, RunUpdate{}, fmt.Errorf("%w: run update created_at", ErrValidation)
	}
	return envelope, update, nil
}

func ValidateACK(raw []byte, trust Trust) (ManagedEnvelope, ACK, error) {
	envelope, err := VerifyEnvelope(raw, trust, "ACK", ACKRevision)
	if err != nil {
		return ManagedEnvelope{}, ACK{}, err
	}
	if err = validateSchema("ack", envelope.Payload); err != nil {
		return ManagedEnvelope{}, ACK{}, fmt.Errorf("%w: ACK schema: %v", ErrValidation, err)
	}
	var ack ACK
	if err = decodeStrict(envelope.Payload, &ack); err != nil || ack.PlatformRunID == "" || ack.AckedSequence < 0 {
		return ManagedEnvelope{}, ACK{}, fmt.Errorf("%w: ACK payload", ErrValidation)
	}
	return envelope, ack, nil
}

func validateClaim(assignment TaskAssignment, trust Trust) error {
	claim := assignment.AuthorizationClaim
	if claim.AssignmentID != assignment.AssignmentID || claim.AssignmentRevision != assignment.AssignmentRevision ||
		claim.NodeID != assignment.NodeID || claim.AgentID != assignment.AgentID || claim.PlatformRunID != assignment.PlatformRunID ||
		claim.RepositoryID != assignment.RepositoryID || !sameOptional(claim.WorktreeID, assignment.WorktreeID) ||
		!equalAccessScope(claim.AccessScope, assignment.AccessScope) || claim.AuthorityScopeID != nil || claim.AuthorityGeneration != nil {
		return fmt.Errorf("%w: claim binding mismatch", ErrValidation)
	}
	if claim.Integrity.Alg != "ed25519" {
		return fmt.Errorf("%w: claim integrity profile", ErrValidation)
	}
	issued, err := parseContractTime(claim.IssuedAt)
	if err != nil {
		return fmt.Errorf("%w: claim issued_at", ErrValidation)
	}
	expires, err := parseContractTime(claim.ExpiresAt)
	if err != nil || !expires.After(issued) || trust.now().Before(issued) || !trust.now().Before(expires) {
		return fmt.Errorf("%w: claim validity", ErrValidation)
	}
	signature, err := base64.RawURLEncoding.DecodeString(claim.Integrity.Value)
	if err != nil {
		return fmt.Errorf("%w: claim integrity encoding", ErrValidation)
	}
	input, err := claimSignatureInput(claim)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(trust.PublicKey), input, signature) {
		return fmt.Errorf("%w: claim integrity", ErrValidation)
	}
	return nil
}

func equalAccessScope(left, right AccessScope) bool {
	if left.Access != right.Access || left.RepositoryID != right.RepositoryID || !sameOptional(left.WorktreeID, right.WorktreeID) ||
		left.GrantedBy != right.GrantedBy || left.GrantedAt != right.GrantedAt || !sameOptional(left.ExpiresAt, right.ExpiresAt) ||
		len(left.Capabilities) != len(right.Capabilities) || len(left.PathPrefixes) != len(right.PathPrefixes) {
		return false
	}
	for i := range left.Capabilities {
		if left.Capabilities[i] != right.Capabilities[i] {
			return false
		}
	}
	for i := range left.PathPrefixes {
		if left.PathPrefixes[i] != right.PathPrefixes[i] {
			return false
		}
	}
	return true
}

func (t Trust) now() time.Time {
	if t.Now != nil {
		return t.Now().UTC()
	}
	return time.Now().UTC()
}

func parseContractTime(value string) (time.Time, error) {
	if len(value) != len("2006-01-02T15:04:05.000Z") || !strings.HasSuffix(value, "Z") {
		return time.Time{}, fmt.Errorf("timestamp must be UTC with milliseconds")
	}
	return time.Parse("2006-01-02T15:04:05.000Z", value)
}

func sameOptional(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
