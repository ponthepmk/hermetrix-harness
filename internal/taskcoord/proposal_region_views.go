package taskcoord

import (
	"errors"
	"hermetrix-harness/internal/worker"
)

func proposalViewsFromRegions(path, source string, regions []proposalSourceRegion, terms []string, limit int) ([]worker.BoundedFileView, error) {
	if err := validateProposalSourceRegions(source, regions); err != nil {
		return nil, err
	}
	if views, err := buildProposalSourceViews(path, source, regions, limit); err == nil {
		return views, nil
	}

	radii := []int{12, 6, 3, 1, 0}
	for _, radius := range radii {
		attempted := make([]proposalSourceRegion, 0, len(regions))
		failed := false
		for _, region := range regions {
			if region.Score == 0 {
				attempted = append(attempted, region)
				continue
			}
			sub, err := proposalLineSourceRegions(source[region.Start:region.End], terms, radius)
			if err != nil {
				failed = true
				break
			}
			for _, r := range sub {
				attempted = append(attempted, proposalSourceRegion{Start: region.Start + r.Start, End: region.Start + r.End})
			}
		}
		if failed {
			continue
		}
		if views, err := buildProposalSourceViews(path, source, attempted, limit); err == nil {
			return views, nil
		}
	}

	return nil, errors.New("could not narrow proposal source regions")
}
