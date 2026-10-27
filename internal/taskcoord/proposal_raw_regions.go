package taskcoord

import (
	"errors"
	"unicode/utf8"
)

func validateProposalSourceRegions(source string, regions []proposalSourceRegion) error {
	if !utf8.ValidString(source) {
		return errors.New("source is not valid UTF-8")
	}
	if len(regions) == 0 {
		return errors.New("regions must be nonempty")
	}
	for _, r := range regions {
		if r.Start < 0 || r.End <= r.Start || r.End > len(source) {
			return errors.New("invalid region bounds")
		}
		if !utf8.ValidString(source[r.Start:r.End]) {
			return errors.New("region is not valid UTF-8")
		}
	}
	return nil
}
