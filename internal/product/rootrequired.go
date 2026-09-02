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
// It is for callers that only have a project ID in hand; a caller that has
// already loaded the Project for other fields should call requireRoot
// directly rather than paying for a second fetch of the same row.
func (s *Service) RequireRoot(ctx context.Context, projectID string) (string, error) {
	project, err := s.GetProject(ctx, projectID)
	if err != nil {
		return "", err
	}
	return requireRoot(project)
}

// requireRoot is the one check behind both RequireRoot and every consumer
// that already holds the Project record. Centralizing it here is the whole
// point: the file, terminal, browser and command paths all read a project's
// root, and a project with no root is not a bug for any of them, so they all
// need to ask the identical question and give the identical, honest answer.
func requireRoot(project Project) (string, error) {
	if project.RootPath == "" {
		return "", fmt.Errorf("%q: %w. Add one in the project's settings, or use a project that has code",
			project.Name, ErrProjectHasNoCode)
	}
	return project.RootPath, nil
}
