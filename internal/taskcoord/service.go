// Package taskcoord coordinates durable, bounded task workflows.
package taskcoord

import (
	"context"
	"errors"
	"fmt"
	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/inference"
	"hermetrix-harness/internal/learning"
	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

const (
	selectionEffect = "provider.select_files"
	proposalEffect  = "provider.propose"
)

var ErrReviewerRequired = errors.New("reviewer_required")

type Service struct {
	tasks               *taskengine.Service
	products            *product.Service
	providers           *providers.Service
	learning            *learning.Service
	brain               KnowledgeRetriever
	brainCompiler       *ctxcompiler.Compiler
	brainLocalProjectID string
	brainProject        string
	skillSource         TaskSkillSource
}

// A multi-file proposal needs enough output for a complete structured call.
// The selected immutable preset is bound to the attempt before dispatch.
func (s *Service) codeProposalPreset(ctx context.Context, profile providers.Profile) (inference.Preset, error) {
	if profile.MaxOutputTokens > 8192 {
		return s.providers.ResolveCodeProposalPreset(ctx, profile)
	}
	for _, id := range []string{"worker-max", "worker-large", "worker-medium"} {
		if preset, err := s.providers.ResolveTaskPreset(ctx, profile, id, 1); err == nil {
			return preset, nil
		}
	}
	return s.providers.ResolveTaskPreset(ctx, profile, "worker", 1)
}

type ProposalInput struct {
	AttemptID  string                  `json:"attempt_id"`
	ProviderID string                  `json:"provider_id"`
	Authority  taskengine.RunAuthority `json:"authority"`
	Packet     taskengine.StepPacket   `json:"packet"`
	Files      []string                `json:"files"`
}

type ProposalOutput struct {
	Result   worker.Result           `json:"result"`
	Artifact product.Artifact        `json:"artifact"`
	Effect   taskengine.EffectIntent `json:"effect"`
	Proposal taskengine.CodeProposal `json:"proposal"`
}

type SelectFilesInput struct {
	AttemptID  string                  `json:"attempt_id"`
	ProviderID string                  `json:"provider_id"`
	Authority  taskengine.RunAuthority `json:"authority"`
}

type SelectFilesOutput struct {
	Result   worker.FileSelectionResult `json:"result"`
	Artifact product.Artifact           `json:"artifact"`
	Effect   taskengine.EffectIntent    `json:"effect"`
}

type ApplyOutput struct {
	Proposal         taskengine.CodeProposal   `json:"proposal"`
	Effect           taskengine.EffectIntent   `json:"effect"`
	Receipts         []product.WriteFileResult `json:"receipts"`
	RollbackArtifact product.Artifact          `json:"rollback_artifact"`
}

type CommandCheck struct {
	ID             string   `json:"id"`
	RequirementIDs []string `json:"requirement_ids,omitempty"`
	Executable     string   `json:"executable"`
	Arguments      []string `json:"arguments"`
	WorkingDir     string   `json:"working_dir"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

type VerifyOutput struct {
	Proposal         taskengine.CodeProposal   `json:"proposal"`
	Jobs             []product.Job             `json:"jobs"`
	Validations      []taskengine.Validation   `json:"validations"`
	Effects          []taskengine.EffectIntent `json:"effects"`
	Evidence         product.Artifact          `json:"evidence"`
	RollbackEvidence product.Artifact          `json:"rollback_evidence,omitempty"`
	RolledBack       bool                      `json:"rolled_back"`
}

type verificationBundle struct {
	Snapshot   product.ProjectSnapshot `json:"source_snapshot"`
	ProposalID string                  `json:"proposal_id"`
	Checks     []CommandCheck          `json:"checks"`
	Jobs       []product.Job           `json:"jobs"`
	Evidence   []taskengine.Validation `json:"evidence"`
}

type ReviewOutput struct {
	Review           worker.ReviewResult     `json:"review"`
	Artifact         product.Artifact        `json:"artifact"`
	RollbackEvidence product.Artifact        `json:"rollback_evidence,omitempty"`
	Proposal         taskengine.CodeProposal `json:"proposal"`
	Task             taskengine.Task         `json:"task"`
	Effect           taskengine.EffectIntent `json:"effect"`
	RolledBack       bool                    `json:"rolled_back"`
}

type ReconcileOutput struct {
	Effect   taskengine.EffectIntent `json:"effect"`
	Job      *product.Job            `json:"job,omitempty"`
	Artifact *product.Artifact       `json:"artifact,omitempty"`
	Outcome  string                  `json:"outcome"`
}

type ReconcileBatch struct {
	Reconciled  []ReconcileOutput         `json:"reconciled"`
	Unsupported []taskengine.EffectIntent `json:"unsupported"`
	Errors      []string                  `json:"errors"`
}

type AutoPlanInput struct {
	TaskID               string `json:"task_id"`
	ExpectedTaskRevision int    `json:"expected_task_revision"`
	ProviderID           string `json:"provider_id"`
	Actor                string `json:"actor"`
}

type AutoPlanOutput struct {
	Result   worker.PlanResult     `json:"result"`
	Run      taskengine.PlannerRun `json:"run"`
	Artifact product.Artifact      `json:"artifact"`
	Task     taskengine.Task       `json:"task"`
}

func New(tasks *taskengine.Service, products *product.Service, providerService *providers.Service) *Service {
	return &Service{tasks: tasks, products: products, providers: providerService}
}

func (s *Service) WithLearning(service *learning.Service) *Service {
	s.learning = service
	if s.tasks != nil {
		if service == nil {
			s.tasks.WithCompletionObserver(nil)
		} else {
			s.tasks.WithCompletionObserver(taskLearningObserver{coordinator: s})
		}
	}
	return s
}

// WithSkills wires a read-only TaskSkillSource for bounded Task Skill selection.
// It does not grant tools or writes; SkillRefs are advisory data only.
func (s *Service) WithSkills(source TaskSkillSource) *Service {
	s.skillSource = source
	return s
}

func validateTaskEgress(task taskengine.Task, profile providers.Profile) error {
	policy := task.EgressPolicy
	if policy == "" {
		policy = "local_only"
	}
	if policy == "local_only" && !providers.IsLocalProfile(profile) {
		return fmt.Errorf("task egress policy is local_only; remote provider dispatch is blocked")
	}
	return nil
}

func minPositive(value, ceiling int) int {
	if value <= 0 || value > ceiling {
		return ceiling
	}
	return value
}
