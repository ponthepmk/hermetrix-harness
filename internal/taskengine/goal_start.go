package taskengine

import (
	"context"
	"errors"
	"hermetrix-harness/internal/identity"
	"strings"
	"unicode/utf8"
)

// CreateGoal creates a goal run for taskID. It clears the config marker and
// delegates to the private createGoalAtID with the identity "goal".
func (s *Service) CreateGoal(ctx context.Context, taskID string, expectedRevision int, actor string, config GoalConfig) (GoalRun, error) {
	config.StartInputSHA256 = ""
	return s.createGoalAtID(ctx, identity.New("goal"), taskID, expectedRevision, actor, config)
}

// CreateGoalAtID creates a goal run with an explicit goalID. It validates the
// goalID, trims the actor, computes the start input digest, and either replays
// an existing goal or creates a new one.
func (s *Service) CreateGoalAtID(ctx context.Context, goalID, taskID string, expectedRevision int, actor string, config GoalConfig) (GoalRun, error) {
	if goalID == "" {
		return GoalRun{}, errors.New("goalID must not be empty")
	}
	if goalID != strings.TrimSpace(goalID) {
		return GoalRun{}, errors.New("goalID must not contain leading or trailing spaces")
	}
	if !utf8.ValidString(goalID) {
		return GoalRun{}, errors.New("goalID must be valid UTF-8")
	}
	if len([]byte(goalID)) > 160 {
		return GoalRun{}, errors.New("goalID must be at most 160 bytes")
	}
	actor = strings.TrimSpace(actor)
	digest, err := goalStartInputSHA256(taskID, expectedRevision, actor, config)
	if err != nil {
		return GoalRun{}, err
	}
	config.StartInputSHA256 = digest

	g, err := s.Goal(ctx, goalID)
	if err == nil {
		return replayGoalStart(g, digest, expectedRevision)
	}
	if !errors.Is(err, ErrNotFound) {
		return GoalRun{}, err
	}

	created, createErr := s.createGoalAtID(ctx, goalID, taskID, expectedRevision, actor, config)
	if createErr == nil {
		return created, nil
	}

	existing, readErr := s.Goal(ctx, goalID)
	if readErr == nil {
		return replayGoalStart(existing, digest, expectedRevision)
	}
	if !errors.Is(readErr, ErrNotFound) {
		return GoalRun{}, readErr
	}

	return GoalRun{}, createErr
}
