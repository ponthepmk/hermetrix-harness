package worker

import "errors"

// validateWorkerWireLowerBound returns a conservative raw-byte lower bound on the
// eventual wire JSON for task. It consumes a fixed 128 KiB budget and returns a
// plain error as soon as any accounted field would exceed it.
func validateWorkerWireLowerBound(task Task) error {
	if err := validateSkillRefs(task.SkillRefs); err != nil {
		return err
	}

	remaining := maxWorkerWireBytes

	consume := func(n int) bool {
		if n < 0 || n > remaining {
			return false
		}
		remaining -= n
		return true
	}

	consumeStrings := func(items ...string) bool {
		for _, s := range items {
			if !consume(len(s)) {
				return false
			}
		}
		return true
	}

	if !consumeStrings(task.ID, task.Kind, task.Brief) {
		return errors.New("task exceeds 128 KiB")
	}

	if !consume(len(task.AcceptanceCriteria)) {
		return errors.New("task exceeds 128 KiB")
	}

	for _, s := range task.AcceptanceCriteria {
		if !consume(len(s)) {
			return errors.New("task exceeds 128 KiB")
		}
	}

	if !consume(len(task.Constraints)) {
		return errors.New("task exceeds 128 KiB")
	}

	for _, s := range task.Constraints {
		if !consume(len(s)) {
			return errors.New("task exceeds 128 KiB")
		}
	}

	if !consume(len(task.RecommendedChecks)) {
		return errors.New("task exceeds 128 KiB")
	}

	for _, s := range task.RecommendedChecks {
		if !consume(len(s)) {
			return errors.New("task exceeds 128 KiB")
		}
	}

	if !consume(len(task.WritablePaths)) {
		return errors.New("task exceeds 128 KiB")
	}

	for _, s := range task.WritablePaths {
		if !consume(len(s)) {
			return errors.New("task exceeds 128 KiB")
		}
	}

	if !consume(len(task.AbsentFiles)) {
		return errors.New("task exceeds 128 KiB")
	}

	for _, s := range task.AbsentFiles {
		if !consume(len(s)) {
			return errors.New("task exceeds 128 KiB")
		}
	}

	if !consume(len(task.ProjectBrainRefs)) {
		return errors.New("task exceeds 128 KiB")
	}

	for _, ref := range task.ProjectBrainRefs {
		if !consumeStrings(ref.Citation, ref.Version, ref.Provenance, ref.Trust, ref.Content) {
			return errors.New("task exceeds 128 KiB")
		}
	}

	if !consume(len(task.SkillRefs)) {
		return errors.New("task exceeds 128 KiB")
	}

	for _, ref := range task.SkillRefs {
		if !consumeStrings(ref.SkillID, ref.VersionID, ref.ContentHash, ref.Name, ref.Content) {
			return errors.New("task exceeds 128 KiB")
		}
	}

	if !consume(len(task.FailureEvidence)) {
		return errors.New("task exceeds 128 KiB")
	}

	for _, rec := range task.FailureEvidence {
		if !consumeStrings(rec.ValidationID, rec.CheckID, rec.SubjectRevision, rec.Actual, rec.ArtifactID, rec.ArtifactSHA256, rec.Output) {
			return errors.New("task exceeds 128 KiB")
		}
	}

	for path, content := range task.Files {
		if _, ok := task.FileViews[path]; ok {
			continue
		}
		if !consume(len(path)) {
			return errors.New("task exceeds 128 KiB")
		}
		if !consume(len(content)) {
			return errors.New("task exceeds 128 KiB")
		}
	}

	for path, views := range task.FileViews {
		if !consume(len(path)) {
			return errors.New("task exceeds 128 KiB")
		}
		if !consume(len(views)) {
			return errors.New("task exceeds 128 KiB")
		}
		for _, view := range views {
			if !consumeStrings(view.Path, view.FullSHA256, view.Content, view.RegionID) {
				return errors.New("task exceeds 128 KiB")
			}
			if !consume(2) {
				return errors.New("task exceeds 128 KiB")
			}
		}
	}

	return nil
}
