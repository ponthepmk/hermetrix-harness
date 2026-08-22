package fidelity

import (
	"time"

	ctxcompiler "hermetrix-harness/internal/context"
)

type Expectations struct {
	EssentialIDs    []string `json:"essential_ids"`
	DecisionIDs     []string `json:"decision_ids"`
	OpenTaskIDs     []string `json:"open_task_ids"`
	FileStateIDs    []string `json:"file_state_ids"`
	CausalPairIDs   []string `json:"causal_pair_ids"`
	TaskAssertions  []string `json:"task_assertions"`
	PatchAssertions []string `json:"patch_assertions"`
	ForbiddenClaims []string `json:"forbidden_claims"`
	MaxTaskDelta    float64  `json:"max_task_delta"`
	MaxPatchDelta   float64  `json:"max_patch_delta"`
}

type CaseInput struct {
	ID             string                 `json:"id,omitempty"`
	Name           string                 `json:"name"`
	Language       string                 `json:"language"`
	BenchmarkClass string                 `json:"benchmark_class"`
	Fragments      []ctxcompiler.Fragment `json:"fragments"`
	Expectations   Expectations           `json:"expectations"`
}

type Case struct {
	CaseInput
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Metrics struct {
	Passed                   bool    `json:"passed"`
	EssentialExactRetention  float64 `json:"essential_exact_retention"`
	DecisionRecall           float64 `json:"decision_recall"`
	OpenTaskRecall           float64 `json:"open_task_recall"`
	FileStateRecall          float64 `json:"file_state_recall"`
	CausalPairSplits         int     `json:"causal_pair_splits"`
	TaskSuccessFull          float64 `json:"task_success_full"`
	TaskSuccessCompiled      float64 `json:"task_success_compiled"`
	TaskSuccessDelta         float64 `json:"task_success_delta"`
	PatchCorrectnessFull     float64 `json:"patch_correctness_full"`
	PatchCorrectnessCompiled float64 `json:"patch_correctness_compiled"`
	PatchCorrectnessDelta    float64 `json:"patch_correctness_delta"`
	HallucinationCount       int     `json:"hallucination_count"`
	FalseSuccessCount        int     `json:"false_success_count"`
	OriginalTokens           int     `json:"original_tokens"`
	CompiledTokens           int     `json:"compiled_tokens"`
	TokensSaved              int     `json:"tokens_saved"`
	CompressionRatio         float64 `json:"compression_ratio"`
	CompileMilliseconds      int64   `json:"compile_milliseconds"`
	PeakHeapDeltaBytes       uint64  `json:"peak_heap_delta_bytes"`
	SilentTruncations        int     `json:"silent_truncations"`
	FallbackUsed             bool    `json:"fallback_used"`
}

type Run struct {
	ID               string     `json:"id"`
	CaseID           string     `json:"case_id"`
	CaseName         string     `json:"case_name"`
	ProfileName      string     `json:"profile_name"`
	CompilerRevision string     `json:"compiler_revision"`
	VerifierRevision string     `json:"verifier_revision"`
	State            string     `json:"state"`
	Metrics          Metrics    `json:"metrics"`
	FullBlobRef      string     `json:"full_blob_ref,omitempty"`
	CompiledBlobRef  string     `json:"compiled_blob_ref,omitempty"`
	FallbackUsed     bool       `json:"fallback_used"`
	Error            string     `json:"error,omitempty"`
	StartedAt        time.Time  `json:"started_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
}
