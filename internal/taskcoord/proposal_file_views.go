package taskcoord

import (
	"errors"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"hermetrix-harness/internal/worker"
)

const maxLimit = 128 * 1024

func proposalFileViews(path, source, stepText string, limit int) ([]worker.BoundedFileView, error) {
	if limit < 1 || limit > maxLimit {
		return nil, errors.New("limit must be between 1 and 128KiB")
	}
	if !utf8.ValidString(source) {
		return nil, errors.New("source is not valid UTF-8")
	}
	terms, err := proposalAnchorTerms(stepText)
	if err != nil {
		return nil, err
	}
	terms, err = proposalAnchorTerms(stepText)
	if err != nil {
		return nil, err
	}
	regions, err := proposalGoSourceRegions(path, source, terms)
	if err == nil && len(regions) > 0 && strings.EqualFold(filepath.Ext(path), ".go") {
		return proposalViewsFromRegions(path, source, regions, terms, limit)
	}
	fullRegion := []proposalSourceRegion{{Start: 0, End: len(source), Score: 1}}
	return proposalViewsFromRegions(path, source, fullRegion, terms, limit)
}
