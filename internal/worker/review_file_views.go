package worker

import (
	"errors"
	"unicode/utf8"
)

// validateReviewFiles performs pure structural validation of review file views.
// It does not claim full-source identity and does not compare full hashes to
// region content hashes.
func validateReviewFiles(full map[string]string, partial map[string]BoundedReviewFile) error {
	for path, content := range full {
		if err := validatePath(path); err != nil {
			return err
		}
		if !utf8.ValidString(content) {
			return errors.New("full review file content is not valid UTF-8")
		}
	}

	for path, file := range partial {
		if err := validatePath(path); err != nil {
			return err
		}
		if _, exists := full[path]; exists {
			return errors.New("partial review file path overlaps full review file path")
		}
		if !isReviewSHA256(file.BeforeSHA256) {
			return errors.New("partial review file BeforeSHA256 is not a 64-character lowercase SHA-256")
		}
		if !isReviewSHA256(file.AfterSHA256) {
			return errors.New("partial review file AfterSHA256 is not a 64-character lowercase SHA-256")
		}
		if len(file.Regions) < 1 || len(file.Regions) > 32 {
			return errors.New("partial review file must contain 1 to 32 regions")
		}

		for i, region := range file.Regions {
			if region.Before.Path != path {
				return errors.New("region Before.Path must match partial review file path")
			}
			if region.Before.FullSHA256 != file.BeforeSHA256 {
				return errors.New("region Before.FullSHA256 must match partial review file BeforeSHA256")
			}
			if region.Before.Start < 0 || region.Before.End < region.Before.Start || region.Before.End > maxWorkerFileBytes {
				return errors.New("region bounds must satisfy 0 <= Start <= End <= maxWorkerFileBytes")
			}
			if region.Before.End-region.Before.Start != len(region.Before.Content) {
				return errors.New("region content length must equal End - Start")
			}
			if !utf8.ValidString(region.Before.Content) {
				return errors.New("region Before.Content is not valid UTF-8")
			}
			if !utf8.ValidString(region.AfterContent) {
				return errors.New("region AfterContent is not valid UTF-8")
			}
			expectedID := hashRegionID(path, file.BeforeSHA256, region.Before.Start, region.Before.End)
			if region.Before.RegionID != expectedID {
				return errors.New("region RegionID does not match hashRegionID")
			}

			for j := 0; j < i; j++ {
				if file.Regions[j].Before.RegionID == region.Before.RegionID {
					return errors.New("duplicate region id")
				}
				if region.Before.Start < file.Regions[j].Before.End &&
					file.Regions[j].Before.Start < region.Before.End &&
					region.Before.End > region.Before.Start &&
					file.Regions[j].Before.End > file.Regions[j].Before.Start {
					return errors.New("overlapping regions")
				}
			}
		}
	}

	return nil
}

// isReviewSHA256 reports whether s is exactly 64 lowercase hexadecimal bytes.
func isReviewSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			continue
		}
		return false
	}
	return true
}
