package taskengine

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestGoalStartRequestChanges(t *testing.T) {
	ctx := context.Background()
	service, _ := testService(t)
	task := createPlannedTask(t, service)
	goalID := task.ID
	cfg := goalConfig()
	orig, err := service.CreateGoalAtID(ctx, goalID, task.ID, task.Revision, "owner", cfg)
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}
	origRun, err := service.Goal(ctx, goalID)
	if err != nil {
		t.Fatalf("goal: %v", err)
	}
	if !reflect.DeepEqual(orig, origRun) {
		t.Fatalf("initial goal mismatch")
	}
	origGoals, err := service.GoalsForTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("goals for task: %v", err)
	}
	if len(origGoals) != 1 {
		t.Fatalf("expected 1 goal, got %d", len(origGoals))
	}
	fields := []string{
		"VerificationTimeoutSeconds",
		"ProjectRoot",
		"ProviderID",
		"ProviderRevision",
		"ReviewerProviderID",
		"ReviewerProviderRevision",
		"ApplyChanges",
		"MaxActions",
		"MaxAttempts",
		"MaxPlans",
		"MaxSeconds",
		"Actor",
		"TaskID",
		"ExpectedRevision",
	}
	for _, name := range fields {
		t.Logf("testing field %s", name)
		cfg := goalConfig()
		taskID := task.ID
		revision := task.Revision
		actor := "owner"
		switch name {
		case "VerificationTimeoutSeconds":
			cfg.VerificationTimeoutSeconds = 999
		case "ProjectRoot":
			cfg.ProjectRoot = "/tmp/other"
		case "ProviderID":
			cfg.ProviderID = "other"
		case "ProviderRevision":
			cfg.ProviderRevision = "other-r2"
		case "ReviewerProviderID":
			cfg.ReviewerProviderID = "other"
		case "ReviewerProviderRevision":
			cfg.ReviewerProviderRevision = "other-r2"
		case "ApplyChanges":
			cfg.ApplyChanges = true
		case "MaxActions":
			cfg.MaxActions = 999
		case "MaxAttempts":
			cfg.MaxAttempts = 999
		case "MaxPlans":
			cfg.MaxPlans = 999
		case "MaxSeconds":
			cfg.MaxSeconds = 999
		case "Actor":
			actor = "other"
		case "TaskID":
			taskID = "other"
		case "ExpectedRevision":
			revision = 999
		}
		got, err := service.CreateGoalAtID(ctx, goalID, taskID, revision, actor, cfg)
		if !errors.Is(err, ErrStaleRevision) {
			t.Fatalf("expected ErrStaleRevision, got %v", err)
		}
		if !reflect.DeepEqual(got, GoalRun{}) {
			t.Fatalf("expected zero GoalRun, got %+v", got)
		}
		cur, err := service.Goal(ctx, goalID)
		if err != nil {
			t.Fatalf("goal after failure: %v", err)
		}
		if !reflect.DeepEqual(cur, orig) {
			t.Fatalf("goal changed after failure")
		}
		curGoals, err := service.GoalsForTask(ctx, task.ID)
		if err != nil {
			t.Fatalf("goals after failure: %v", err)
		}
		if len(curGoals) != 1 {
			t.Fatalf("expected 1 goal after failure, got %d", len(curGoals))
		}
	}
}
