package taskcoord

import (
	"context"
	"fmt"
	"strings"

	"hermetrix-harness/internal/taskengine"
)

// resolveCodingGoalConfig resolves the frozen configuration for a coding goal.
// It performs metadata resolution only and does not create a Goal or Task.
func (s *Service) resolveCodingGoalConfig(ctx context.Context, task taskengine.Task, in StartGoalInput) (taskengine.GoalConfig, error) {
	if task.ProjectID == "" {
		return taskengine.GoalConfig{}, fmt.Errorf("coding goals require a project")
	}
	projectRoot, err := s.products.ProjectRootIdentity(ctx, task.ProjectID)
	if err != nil {
		return taskengine.GoalConfig{}, err
	}
	writer, err := s.providers.Get(ctx, in.ProviderID)
	if err != nil {
		return taskengine.GoalConfig{}, err
	}
	reviewer, err := s.providers.Get(ctx, in.ReviewerProviderID)
	if err != nil {
		return taskengine.GoalConfig{}, err
	}
	if !writer.Enabled || !reviewer.Enabled {
		return taskengine.GoalConfig{}, fmt.Errorf("both goal providers must be enabled")
	}
	if err = validateTaskEgress(task, writer); err != nil {
		return taskengine.GoalConfig{}, err
	}
	if err = validateTaskEgress(task, reviewer); err != nil {
		return taskengine.GoalConfig{}, err
	}
	actor := strings.TrimSpace(in.Actor)
	if actor == "" || len([]rune(actor)) > 200 {
		return taskengine.GoalConfig{}, fmt.Errorf("actor must be a nonempty string of at most 200 runes")
	}
	if writer.ID == reviewer.ID {
		return taskengine.GoalConfig{}, fmt.Errorf("writer and reviewer provider IDs must be distinct")
	}
	if in.MaxActions == 0 {
		in.MaxActions = 100
	}
	if in.MaxAttempts == 0 {
		in.MaxAttempts = 8
	}
	if in.MaxPlans == 0 {
		in.MaxPlans = 3
	}
	if in.MaxSeconds == 0 {
		in.MaxSeconds = 3600
	}
	if in.VerificationTimeoutSeconds == 0 {
		in.VerificationTimeoutSeconds = 120
	}
	if in.MaxActions < 1 || in.MaxActions > 10000 {
		return taskengine.GoalConfig{}, fmt.Errorf("max_actions must be 1–10000")
	}
	if in.MaxAttempts < 1 || in.MaxAttempts > 128 {
		return taskengine.GoalConfig{}, fmt.Errorf("max_attempts must be 1–128")
	}
	if in.MaxPlans < 1 || in.MaxPlans > 32 {
		return taskengine.GoalConfig{}, fmt.Errorf("max_plans must be 1–32")
	}
	if in.MaxSeconds < 1 || in.MaxSeconds > 86400 {
		return taskengine.GoalConfig{}, fmt.Errorf("max_seconds must be 1–86400")
	}
	if in.VerificationTimeoutSeconds < 1 || in.VerificationTimeoutSeconds > 86400 {
		return taskengine.GoalConfig{}, fmt.Errorf("verification_timeout_seconds must be 1–86400")
	}
	return taskengine.GoalConfig{
		VerificationTimeoutSeconds: in.VerificationTimeoutSeconds,
		ProjectRoot:                projectRoot,
		ProviderID:                 writer.ID,
		ProviderRevision:           writer.Revision,
		ReviewerProviderID:         reviewer.ID,
		ReviewerProviderRevision:   reviewer.Revision,
		ApplyChanges:               in.ApplyChanges,
		MaxActions:                 in.MaxActions,
		MaxAttempts:                in.MaxAttempts,
		MaxPlans:                   in.MaxPlans,
		MaxSeconds:                 in.MaxSeconds,
	}, nil
}
