package taskengine

import (
	"context"
	"reflect"
	"testing"
)

func TestGoalStartRejectsInvalidUTF8(t *testing.T) {
	bad := string([]byte{255})
	subtests := []struct {
		name   string
		mutate func(cfg *GoalConfig)
	}{
		{"actor", func(c *GoalConfig) {}},
		{"ProjectRoot", func(c *GoalConfig) { c.ProjectRoot = bad }},
		{"ProviderID", func(c *GoalConfig) { c.ProviderID = bad }},
		{"ProviderRevision", func(c *GoalConfig) { c.ProviderRevision = bad }},
		{"ReviewerProviderID", func(c *GoalConfig) { c.ReviewerProviderID = bad }},
		{"ReviewerProviderRevision", func(c *GoalConfig) { c.ReviewerProviderRevision = bad }},
	}
	for _, sub := range subtests {
		t.Run(sub.name, func(t *testing.T) {
			service, _ := testService(t)
			task := createPlannedTask(t, service)
			cfg := goalConfig()
			actor := "owner"
			if sub.name == "actor" {
				actor = bad
			}
			sub.mutate(&cfg)
			goalID := "goal-utf8-bad"
			record, err := service.CreateGoalAtID(context.Background(), goalID, task.ID, task.Revision, actor, cfg)
			if err == nil {
				t.Fatalf("expected error for invalid UTF-8 %s", sub.name)
			}
			if !reflect.DeepEqual(record, GoalRun{}) {
				t.Fatalf("expected empty GoalRun, got %+v", record)
			}
			goals, gerr := service.GoalsForTask(context.Background(), task.ID)
			if gerr != nil {
				t.Fatalf("GoalsForTask: %v", gerr)
			}
			if len(goals) != 0 {
				t.Fatalf("expected no goals, got %d", len(goals))
			}
		})
	}
}
