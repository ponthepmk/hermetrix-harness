package taskcoord

import (
	"errors"
	"unicode/utf8"

	"hermetrix-harness/internal/worker"
)

func buildProposalSourceViews(path, source string, regions []proposalSourceRegion, limit int) ([]worker.BoundedFileView, error) {
	if limit < 1 || limit > 128*1024 {
		return nil, errors.New("limit must be between 1 and 128KiB")
	}
	if !utf8.ValidString(source) {
		return nil, errors.New("source is not valid UTF-8")
	}
	if err := validateProposalSourceRegions(source, regions); err != nil {
		return nil, err
	}
	merged := mergeProposalRegions(regions)
	if len(merged) == 0 || len(merged) > 32 {
		return nil, errors.New("merged regions must be nonempty and at most 32")
	}
	for _, r := range merged {
		if r.Start < 0 || r.End <= r.Start || r.End > len(source) {
			return nil, errors.New("invalid region interval")
		}
	}
	remaining := limit
	for _, r := range merged {
		if r.End-r.Start > remaining {
			return nil, errors.New("aggregated content exceeds limit")
		}
		remaining -= r.End - r.Start
	}
	sha := worker.Hash(source)
	views := make([]worker.BoundedFileView, 0, len(merged))
	for _, r := range merged {
		view, err := worker.NewBoundedFileView(path, source, sha, r.Start, r.End)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}
