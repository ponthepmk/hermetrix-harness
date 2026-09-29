package product

import (
	"context"
	"os/exec"
	"strings"
)

// ProjectHasGitRepository checks the exact workspace path that command checks
// will use. A missing git executable or an ordinary directory is not a Git
// repository; neither condition should become a frozen mandatory check.
func (s *Service) ProjectHasGitRepository(ctx context.Context, projectID string) (bool, error) {
	project, err := s.GetProject(ctx, projectID)
	if err != nil {
		return false, err
	}
	root, err := requireRoot(project)
	if err != nil {
		return false, err
	}
	command := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--is-inside-work-tree")
	output, err := command.Output()
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	return err == nil && strings.TrimSpace(string(output)) == "true", nil
}
