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
			if assertionCommand(argv) {
				if _, err := compileFileAssertion(argv); err != nil {
					return err
				}
				if !available["node"] {
					return fmt.Errorf("file assertions require the available node runtime")
				}
				if argv[0] == "assert-file-sha256" {
					found := false
					for _, file := range task.RepositoryFiles {
						if file.Path == argv[1] && file.SHA256 == argv[2] {
							found = true
						}
					}
					if !found {
						return fmt.Errorf("SHA256 assertion must bind the observed repository snapshot for %q", argv[1])
					}
				}
				continue
			}
			if !available[argv[0]] {
				return fmt.Errorf("plan check %q in step %q uses unavailable executable %q; revise the plan before starting work", check, step.Key, argv[0])
			}
			if strings.EqualFold(argv[0], "git") && !task.GitRepository {
				return fmt.Errorf("plan check %q in step %q requires a Git repository, but this project is not one; choose a project test command instead", check, step.Key)
			}
			if len(argv) > 1 && ((argv[0] == "git" && argv[1] == "diff") || argv[0] == "rg" || argv[0] == "ls") {
				return fmt.Errorf("plan check %q is an observation, not an acceptance assertion; use a file assertion or actual test", check)
			}
			if argv[0] == "node" && len(argv) > 2 && argv[1] == "-e" && strings.Contains(argv[2], "createHash") {
				return fmt.Errorf("inline hash code is not a host-verified assertion; use assert-file-sha256 with the observed snapshot")
			}
		}
	}
	return nil
}
