package worker

import (
	"errors"
)

// validateBoundedViewSet validates the supplied bounded views against the
// expected path, full source, and expected full SHA-256. It checks the view
// count, each view's integrity, region ids, duplicates, and positive-length
// overlaps without changing their behavior or messages.
func validateBoundedViewSet(expectedPath, fullSource, expectedFullSHA string, views []BoundedFileView) error {
	if len(views) == 0 {
		return errors.New("no edit permission without at least one bounded view")
	}
	if len(views) > 32 {
		return errors.New("a change may contain at most 32 bounded views")
	}
	for _, view := range views {
		if err := ValidateBoundedFileView(expectedPath, fullSource, expectedFullSHA, view); err != nil {
			return err
		}
	}
	for _, view := range views {
		if view.RegionID == "" {
			return errors.New("bounded view has no region id")
		}
	}
	for i := 0; i < len(views); i++ {
		for j := i + 1; j < len(views); j++ {
			if views[i].RegionID == views[j].RegionID {
				return errors.New("duplicate bounded view region id")
			}
			if views[i].Start < views[j].End && views[j].Start < views[i].End && views[i].End > views[i].Start && views[j].End > views[j].Start {
				return errors.New("overlapping bounded views")
			}
		}
	}
	return nil
}
