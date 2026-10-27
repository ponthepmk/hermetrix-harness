package worker

import (
	"errors"
	"strings"
)

// BuildReviewFiles derives read-only review evidence from a validated result.
// It never mutates caller state and returns nil maps on any error.
func BuildReviewFiles(task Task, result Result) (map[string]string, map[string]BoundedReviewFile, error) {
	frozen, _, _, err := FreezeTask(task)
	if err != nil {
		return nil, nil, err
	}
	if err := ValidateResult(frozen, result); err != nil {
		return nil, nil, err
	}

	wholeFiles := make(map[string]string)
	reviewFiles := make(map[string]BoundedReviewFile)

	for _, change := range result.Changes {
		before, ok := frozen.Files[change.Path]
		if !ok {
			return nil, nil, errors.New("review builder: missing preimage")
		}
		beforeSHA := Hash(before)

		views, hasViews := frozen.FileViews[change.Path]
		if !hasViews || len(views) == 0 {
			wholeFiles[change.Path] = change.Content
			continue
		}

		type resolved struct {
			start, end int
		}
		var res []resolved
		for _, e := range change.Edits {
			if e.Old == "" {
				return nil, nil, errors.New("review builder: empty preimage")
			}
			start := strings.Index(before, e.Old)
			if start < 0 {
				return nil, nil, errors.New("review builder: edit preimage not found")
			}
			end := start + len(e.Old)
			res = append(res, resolved{start: start, end: end})
		}

		regions := make([]BoundedReviewRegion, 0, len(views))
		for _, view := range views {
			var localEdits []TextEdit
			for _, r := range res {
				if view.Start <= r.start && r.end <= view.End {
					for _, e := range change.Edits {
						if e.Old == before[r.start:r.end] {
							localEdits = append(localEdits, e)
							break
						}
					}
				}
			}

			if len(localEdits) == 0 {
				regions = append(regions, BoundedReviewRegion{
					Before:       view,
					AfterContent: view.Content,
				})
				continue
			}

			viewSource := view.Content
			viewSHA := Hash(viewSource)
			fullView, err := NewBoundedFileView(change.Path, viewSource, viewSHA, 0, len(viewSource))
			if err != nil {
				return nil, nil, err
			}
			rederived, _, err := MaterializeBoundedChange(change.Path, viewSource, viewSHA, []BoundedFileView{fullView}, nil, localEdits)
			if err != nil {
				return nil, nil, err
			}
			regions = append(regions, BoundedReviewRegion{
				Before:       view,
				AfterContent: rederived,
			})
		}

		reviewFiles[change.Path] = BoundedReviewFile{
			BeforeSHA256: beforeSHA,
			AfterSHA256:  Hash(change.Content),
			Regions:      regions,
		}
	}

	return wholeFiles, reviewFiles, nil
}
