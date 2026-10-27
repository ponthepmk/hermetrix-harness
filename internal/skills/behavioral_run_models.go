package skills

import (
	"errors"
	"time"
)

// BehavioralRunManifest is the pinned manifest for a behavioral run.
// It records the candidate, base, and binding metadata that a trusted host
// runner will later use to build the actual run. It does not authorize
// promotion or record any observed evaluation outcome.
//
// Pinned metadata:
//
//   - CandidateHash, CandidateID, CandidateBlobRef, CandidateRevision
//   - BaseBlobRef, BaseVersionID, BaseHash
//   - OwnerPrincipalID, ScopeRef, ScopeKind
//   - Bindings
//
// This manifest is not an observed evaluation result.
//
// BehavioralRunManifest is the pinned manifest for a behavioral run.
// It records the candidate, base, and binding metadata that a trusted host
// runner will later use to build the actual run. It does not authorize
// promotion or record any observed evaluation outcome.
//
// Pinned metadata:
//
//   - CandidateHash, CandidateID, CandidateBlobRef, CandidateRevision
//   - BaseBlobRef, BaseVersionID, BaseHash
//   - OwnerPrincipalID, ScopeRef, ScopeKind
//   - Bindings
//
// This manifest is not an observed evaluation result.
type BehavioralRunManifest struct {
	CandidateHash     string                `json:"candidate_hash"`
	OwnerPrincipalID  string                `json:"owner_principal_id"`
	ScopeRef          string                `json:"scope_ref"`
	ScopeKind         string                `json:"scope_kind"`
	CandidateID       string                `json:"candidate_id"`
	BaseBlobRef       string                `json:"base_blob_ref"`
	CandidateBlobRef  string                `json:"candidate_blob_ref"`
	BaseVersionID     string                `json:"base_version_id"`
	CandidateRevision int                   `json:"candidate_revision"`
	BaseHash          string                `json:"base_hash"`
	Bindings          BehavioralRunBindings `json:"bindings"`
}

// BehavioralRun is the observed run record. It carries the manifest plus
// execution metadata such as state, timestamps, and result hashes.
type BehavioralRun struct {
	ID             string                `json:"id"`
	ResultSHA256   string                `json:"result_sha256"`
	StartedAt      time.Time             `json:"started_at"`
	State          string                `json:"state"`
	EvaluationID   string                `json:"evaluation_id"`
	ManifestSHA256 string                `json:"manifest_sha256"`
	CompletedAt    *time.Time            `json:"completed_at,omitempty"`
	Manifest       BehavioralRunManifest `json:"manifest"`
}

// BehavioralRunBindings is the pinned binding metadata for a behavioral run.
// It is built by a trusted host runner later; it does not itself execute
// or authorize promotion.
type BehavioralRunBindings struct {
	ProjectID         string   `json:"project_id"`
	SessionID         string   `json:"session_id"`
	BudgetSHA256      string   `json:"budget_sha256"`
	EnvironmentSHA256 string   `json:"environment_sha256"`
	ProviderID        string   `json:"provider_id"`
	CaseIDs           []string `json:"case_ids"`
	Model             string   `json:"model"`
	ProviderRevision  string   `json:"provider_revision"`
	PresetRevision    int      `json:"preset_revision"`
	CatalogSHA256     string   `json:"catalog_sha256"`
	DatasetSHA256     string   `json:"dataset_sha256"`
	PresetID          string   `json:"preset_id"`
	ModelConfigSHA256 string   `json:"model_config_sha256"`
	EvaluatorRevision string   `json:"evaluator_revision"`
	DatasetSplit      string   `json:"dataset_split"`
}

// BehavioralRun state constants.
const (
	BehavioralRunPrepared   = "prepared"
	BehavioralRunDispatched = "dispatched"
	BehavioralRunCompleted  = "completed"
	BehavioralRunUncertain  = "uncertain"
	BehavioralRunStale      = "stale"
	BehavioralRunAborted    = "aborted"
)

// Sentinel errors for behavioral run validation.
var (
	ErrBehavioralRunBindings  = errors.New("invalid behavioral run bindings")
	ErrBehavioralRunUncertain = errors.New("behavioral run execution outcome is uncertain")
)
