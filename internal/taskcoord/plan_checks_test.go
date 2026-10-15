package taskcoord

import (
	"strings"
	"testing"

	"hermetrix-harness/internal/worker"
)

func TestValidatePlannedChecksRejectsUnavailableProjectCapability(t *testing.T) {
	task := worker.PlanTask{AllowedExecutables: []string{"git", "node"}, GitRepository: false}
	result := worker.PlanResult{Steps: []worker.PlannedStep{{Key: "fix", Checks: []string{"git diff --name-only", "node test.js"}}}}
	if err := validatePlannedChecks(result, task); err == nil || !strings.Contains(err.Error(), "not one") {
		t.Fatalf("no-Git project accepted frozen Git check: %v", err)
	}
	result.Steps[0].Checks = []string{"node test.js"}
	if err := validatePlannedChecks(result, task); err != nil {
		t.Fatalf("project test should remain valid: %v", err)
	}
	result.Steps[0].Checks = []string{"npm test"}
	if err := validatePlannedChecks(result, task); err == nil || !strings.Contains(err.Error(), "unavailable executable") {
		t.Fatalf("unavailable executable accepted: %v", err)
	}
	result.Steps[0].Checks = []string{"node test.js && echo passed"}
	if err := validatePlannedChecks(result, task); err == nil || !strings.Contains(err.Error(), "shell control") {
		t.Fatalf("shell syntax accepted: %v", err)
	}
	task.GitRepository = true
	result.Steps[0].Checks = []string{"git diff --name-only"}
	if err := validatePlannedChecks(result, task); err == nil || !strings.Contains(err.Error(), "observation") {
		t.Fatalf("Git diff incorrectly accepted as proof: %v", err)
	}
}
