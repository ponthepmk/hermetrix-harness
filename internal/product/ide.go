package product

import (
	"context"
	"fmt"
	"go/format"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// IDECapabilities describes installed command tools independently of PTY support.
type IDECapabilities struct {
	Tools map[string]bool `json:"tools"`
}

func (s *Service) ProjectIDECapabilities(ctx context.Context, projectID string) (IDECapabilities, error) {
	project, err := s.GetProject(ctx, projectID)
	if err != nil {
		return IDECapabilities{}, err
	}
	if _, err = requireRoot(project); err != nil {
		return IDECapabilities{}, err
	}
	result := IDECapabilities{Tools: map[string]bool{}}
	for _, name := range AllowedCommandExecutables() {
		_, err := exec.LookPath(name)
		result.Tools[name] = err == nil
	}
	return result, nil
}

type IDEFormatInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// FormatIDEBuffer returns an edit. The normal SHA-checked Save path still owns
// disk writes, so formatting an unsaved buffer remains undoable and race safe.
func (s *Service) FormatIDEBuffer(ctx context.Context, projectID string, input IDEFormatInput) (IDEFormatInput, error) {
	project, err := s.GetProject(ctx, projectID)
	if err != nil {
		return IDEFormatInput{}, err
	}
	root, err := requireRoot(project)
	if err != nil {
		return IDEFormatInput{}, err
	}
	if !utf8.ValidString(input.Path) || strings.ContainsRune(input.Path, 0) {
		return IDEFormatInput{}, fmt.Errorf("file path must be valid UTF-8 without NUL")
	}
	clean := filepath.Clean(strings.TrimSpace(input.Path))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return IDEFormatInput{}, fmt.Errorf("file path must stay inside the project root")
	}
	if _, err := resolveInside(root, filepath.Dir(clean), true); err != nil {
		return IDEFormatInput{}, err
	}
	if len(input.Content) > maxWorkbenchFileBytes || !utf8.ValidString(input.Content) || strings.ContainsRune(input.Content, 0) {
		return IDEFormatInput{}, fmt.Errorf("formatter requires UTF-8 text up to 2 MiB")
	}
	if !strings.EqualFold(filepath.Ext(clean), ".go") {
		return IDEFormatInput{}, fmt.Errorf("server formatter supports Go files; use the bundled formatter for web files")
	}
	formatted, err := format.Source([]byte(input.Content))
	if err != nil {
		return IDEFormatInput{}, fmt.Errorf("Go syntax: %w", err)
	}
	return IDEFormatInput{Path: filepath.ToSlash(clean), Content: string(formatted)}, nil
}

// IDEJob adds a bounded live output snapshot while keeping the original receipt
// authoritative once the process finishes. A different project's job is refused.
func (s *Service) IDEJob(ctx context.Context, projectID, jobID string) (Job, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return Job{}, err
	}
	job, err := s.GetJob(ctx, jobID)
	if err != nil {
		return Job{}, err
	}
	if job.Payload["project_id"] != projectID {
		return Job{}, fmt.Errorf("job does not belong to this project")
	}
	if job.State == "queued" || job.State == "running" {
		s.mu.Lock()
		buffer := s.commandOutputs[jobID]
		s.mu.Unlock()
		if buffer != nil {
			buffer.mu.Lock()
			output, truncated := buffer.buffer.String(), buffer.truncated
			buffer.mu.Unlock()
			if job.Result == nil {
				job.Result = map[string]any{}
			}
			job.Result["output"], job.Result["truncated"] = output, truncated
		}
	}
	return job, nil
}
