package taskengine

import (
	"context"
	"reflect"
	"testing"
)

func TestGoalStartUnicodeIdentity(t *testing.T) {
	s, _ := testService(t)
	task := createPlannedTask(t, s)
	cfg := goalConfig()
	cfg.ProjectRoot = "โครงการ"
	cfg.StartInputSHA256 = string([]byte{255})

	ctx := context.Background()
	first, err := s.CreateGoalAtID(ctx, "goal-ภาษาไทย", task.ID, task.Revision, "เจ้าของ", cfg)
	if err != nil {
		t.Fatalf("first CreateGoalAtID: %v", err)
	}
	if len(first.Config.StartInputSHA256) != 64 {
		t.Fatalf("first marker length = %d", len(first.Config.StartInputSHA256))
	}

	second, err := s.CreateGoalAtID(ctx, "goal-ภาษาไทย", task.ID, task.Revision, "  เจ้าของ  ", cfg)
	if err != nil {
		t.Fatalf("second CreateGoalAtID: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("records differ")
	}

	goals, err := s.GoalsForTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GoalsForTask: %v", err)
	}
	if len(goals) != 1 {
		t.Fatalf("goals len = %d", len(goals))
	}
}
