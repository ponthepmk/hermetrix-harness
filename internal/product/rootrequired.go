package product

import (
	"context"
	"errors"
	"fmt"
)

// A project is a bounded scope, and code is optional inside it: planning a trip
// and planning a refactor have the same shape, and only one of them has files.
//
// Every tool that does need files asks here, so the answer is one sentence in
// one place. Spreading it across the file, terminal, browser and command paths
// would be four answers to one question, and one day they would not match.

// ErrProjectHasNoCode reports a project that has no code folder. It is distinct
// from a path that is wrong: "you have not given this project a folder" and
// "that folder is not there" are different problems with different fixes.
var ErrProjectHasNoCode = errors.New("this project has no code folder")

// RequireRoot returns the project's code root, or explains that there is none.
func (s *Service) RequireRoot(ctx context.Context, projectID string) (string, error) {
	project, err := s.GetProject(ctx, projectID)
	if err != nil {
		return "", err
	}
	if project.RootPath == "" {
		return "", fmt.Errorf("%q: %w. Add one in the project's settings, or use a project that has code",
			project.Name, ErrProjectHasNoCode)
	}
	return project.RootPath, nil
}
