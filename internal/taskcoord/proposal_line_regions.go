package taskcoord

import (
	"errors"
	"strings"
)

func proposalLineSourceRegions(source string, terms []string, radius int) ([]proposalSourceRegion, error) {
	if radius < 0 || radius > 32 {
		return nil, errors.New("invalid radius")
	}
	lines := strings.SplitAfter(source, "\n")
	cumulative := make([]int, len(lines)+1)
	for i, line := range lines {
		cumulative[i+1] = cumulative[i] + len(line)
	}
	best := 0
	for _, line := range lines {
		score := proposalRegionScore(line, terms)
		if score > best {
			best = score
		}
	}
	if best <= 0 {
		return nil, errors.New("no source anchor")
	}
	var selected []proposalSourceRegion
	for i, line := range lines {
		score := proposalRegionScore(line, terms)
		if best >= 50 && score >= 50 || best < 50 && score == best {
			start := i - radius
			if start < 0 {
				start = 0
			}
			end := i + radius + 1
			if end > len(lines) {
				end = len(lines)
			}
			selected = append(selected, proposalSourceRegion{
				Start: cumulative[start],
				End:   cumulative[end],
				Score: score,
			})
		}
	}
	merged := mergeProposalRegions(selected)
	if len(merged) > 32 {
		return nil, errors.New("too many merged intervals")
	}
	if len(merged) == 0 {
		return nil, errors.New("no source regions")
	}
	return merged, nil
}
