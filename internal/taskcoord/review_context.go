package taskcoord

import (
	"errors"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/worker"
)

func proposalReviewFiles(s *Service, profile providers.Profile, original *worker.Task, result worker.Result, verifiedAfter map[string]string) (map[string]string, map[string]worker.BoundedReviewFile, error) {
	if s == nil || s.providers == nil {
		return nil, nil, errors.New("proposal review requires service providers")
	}
	for path, source := range verifiedAfter {
		if s.providers.CredentialAppearsIn(profile, path, source) {
			return nil, nil, errors.New("credential present in full review source")
		}
	}
	if original != nil {
		for path, source := range original.Files {
			if s.providers.CredentialAppearsIn(profile, path, source) {
				return nil, nil, errors.New("credential present in full review source")
			}
		}
	}
	for _, change := range result.Changes {
		if content, ok := verifiedAfter[change.Path]; !ok || content != change.Content {
			return nil, nil, errors.New("verified review source differs from proposal")
		}
	}
	if original == nil {
		return verifiedAfter, nil, nil
	}
	return worker.BuildReviewFiles(*original, result)
}
