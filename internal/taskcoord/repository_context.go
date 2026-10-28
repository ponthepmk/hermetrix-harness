package taskcoord

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

func manifestReceipt(m product.TaskManifest, supplied int) map[string]any {
	return map[string]any{"mode": "ranked_paths", "visited_entries": m.VisitedEntries, "source_paths_seen": m.SourcePathsSeen, "metadata_reads": m.MetadataReads, "supplied_files": supplied, "truncated": m.Truncated || supplied < len(m.Files)}
}

// The provider's existing preflight estimates four characters per token. Use
// bytes conservatively and reserve 2048 tokens for system/tool/schema framing.
// The provider still validates the rendered request before network dispatch.
func repositoryBodyBudget(inputTokens, ceiling int) int {
	if inputTokens <= 2048 {
		return 0
	}
	return min(ceiling, inputTokens-2048)
}

func (s *Service) plannerRepositoryContext(ctx context.Context, task taskengine.Task, input *worker.PlanTask, inputTokens int) (map[string]any, error) {
	input.RepositoryDiscoveryMode = "ranked_paths"
	query := task.Title + "\n" + task.Objective + "\n" + task.OriginalRequest + "\n" + strings.Join(task.Requirement.Constraints, "\n")
	for _, criterion := range task.Requirement.Criteria {
		query += "\n" + criterion.Description
	}
	m, err := s.products.ProjectTaskManifest(ctx, task.ProjectID, query, task.Requirement.Contract.WritablePaths, 512)
	if err != nil {
		return nil, err
	}
	input.RepositoryFiles = nil
	base, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	// Reserve the array/flags plus optional 64-character digest field for each
	// included file. Marshal each entry once, before reading its bytes for a hash.
	size := len(base) + 128
	bodyLimit := repositoryBodyBudget(inputTokens, 48<<10)
	for _, entry := range m.Files {
		file := worker.PlanFile{Path: entry.Path, Bytes: entry.Bytes, Absent: entry.Absent}
		if entry.Absent {
			file.SHA256 = "absent"
		}
		encoded, err := json.Marshal(file)
		if err != nil {
			return nil, err
		}
		reserve := len(encoded) + 1
		if !entry.Absent {
			reserve += 80
		}
		if size+reserve > bodyLimit {
			break
		}
		size += reserve
		input.RepositoryFiles = append(input.RepositoryFiles, file)
	}
	if len(input.RepositoryFiles) < len(task.Requirement.Contract.WritablePaths) {
		return nil, fmt.Errorf("planner context cannot retain every explicitly writable source path")
	}
	fitted := m
	fitted.Files = m.Files[:len(input.RepositoryFiles)]
	hashes, err := s.products.HashTaskManifest(ctx, task.ProjectID, fitted, product.MaxPlannerSnapshotBytes)
	if err != nil {
		return nil, err
	}
	for i := range input.RepositoryFiles {
		input.RepositoryFiles[i].SHA256 = hashes.Hashes[input.RepositoryFiles[i].Path]
	}
	input.RepositoryFilesTruncated = m.Truncated || len(input.RepositoryFiles) < len(m.Files)
	input.RepositoryHashesTruncated = hashes.Truncated
	receipt := manifestReceipt(m, len(input.RepositoryFiles))
	receipt["snapshot_bytes"] = hashes.BytesRead
	receipt["hashes_truncated"] = hashes.Truncated
	receipt["input_body_byte_limit"] = bodyLimit
	return receipt, nil
}

func (s *Service) selectionRepositoryContext(ctx context.Context, projectID string, writable, checks []string, input *worker.FileSelectionTask, inputTokens int) (map[string]any, error) {
	query := input.Objective + "\n" + input.StepTitle + "\n" + input.StepInstructions + "\n" + strings.Join(input.AcceptanceCriteria, "\n") + "\n" + strings.Join(checks, "\n")
	m, err := s.products.ProjectTaskManifest(ctx, projectID, query, writable, 2000)
	if err != nil {
		return nil, err
	}
	input.RepositoryDiscoveryMode = "ranked_paths"
	input.Candidates = nil
	base, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	size := len(base) + 128
	bodyLimit := repositoryBodyBudget(inputTokens, 120<<10)
	for _, entry := range m.Files {
		candidate := worker.FileCandidate{Path: entry.Path, Bytes: entry.Bytes, Absent: entry.Absent}
		encoded, err := json.Marshal(candidate)
		if err != nil {
			return nil, err
		}
		if size+len(encoded)+1 > bodyLimit {
			break
		}
		size += len(encoded) + 1
		input.Candidates = append(input.Candidates, candidate)
	}
	if len(input.Candidates) == 0 || len(input.Candidates) < len(writable) {
		return nil, fmt.Errorf("file selector context cannot retain its required source scope")
	}
	input.CandidatesTruncated = m.Truncated || len(input.Candidates) < len(m.Files)
	receipt := manifestReceipt(m, len(input.Candidates))
	receipt["input_body_byte_limit"] = bodyLimit
	return receipt, nil
}
