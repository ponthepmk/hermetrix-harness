package taskcoord

import "sort"

// proposalSourceRegion describes a source region proposed by the host parser.
// Start and End are inclusive/exclusive host source intervals; Score ranks the region.
type proposalSourceRegion struct {
	Start int
	End   int
	Score int
}

// mergeProposalRegions merges overlapping or touching proposal source regions.
// It returns nil for empty input and does not mutate the input slice.
func mergeProposalRegions(input []proposalSourceRegion) []proposalSourceRegion {
	if len(input) == 0 {
		return nil
	}

	regions := make([]proposalSourceRegion, len(input))
	copy(regions, input)

	sort.Slice(regions, func(i, j int) bool {
		if regions[i].Start != regions[j].Start {
			return regions[i].Start < regions[j].Start
		}
		return regions[i].End < regions[j].End
	})

	var merged []proposalSourceRegion
	for _, region := range regions {
		if len(merged) == 0 {
			merged = append(merged, region)
			continue
		}

		last := &merged[len(merged)-1]
		if region.Start <= last.End {
			if region.End > last.End {
				last.End = region.End
			}
			if region.Score > last.Score {
				last.Score = region.Score
			}
		} else {
			merged = append(merged, region)
		}
	}

	return merged
}
