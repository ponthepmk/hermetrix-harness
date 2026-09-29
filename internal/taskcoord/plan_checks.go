package taskcoord

import (
	"fmt"
	"strings"

	"hermetrix-harness/internal/worker"
)

// validatePlannedChecks runs before CreatePlan freezes commands into a step.
// It validates only facts known before implementation; a real test failure
// after the change still belongs to the normal rollback and review flow.
func validatePlannedChecks(result worker.PlanResult, task worker.PlanTask) error {
	available := make(map[string]bool, len(task.AllowedExecutables))
	for _, executable := range task.AllowedExecutables {
		available[executable] = true
	}
	for _, step := range result.Steps {
		for _, check := range step.Checks {
			argv, err := parseFrozenCommand(check)
			if err != nil {
				return fmt.Errorf("plan check %q in step %q cannot run: %w; revise the plan before starting work", check, step.Key, err)
			}
			if !available[argv[0]] {
				return fmt.Errorf("plan check %q in step %q uses unavailable executable %q; revise the plan before starting work", check, step.Key, argv[0])
			}
			if strings.EqualFold(argv[0], "git") && !task.GitRepository {
				return fmt.Errorf("plan check %q in step %q requires a Git repository, but this project is not one; choose a project test command instead", check, step.Key)
			}
		}
	}
	return nil
}
