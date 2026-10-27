package worker

import "errors"

func ValidateTaskSourceResources(task Task) error {
	if len(task.Files) < 1 || len(task.Files) > 32 {
		return errors.New("task must contain 1 to 32 files")
	}

	remainingSource := maxWorkerSourceBytes
	for _, content := range task.Files {
		if len(content) > maxWorkerFileBytes {
			return errors.New("file source exceeds 2 MiB")
		}
		if len(content) > remainingSource {
			return errors.New("aggregate source exceeds 8 MiB")
		}
		remainingSource -= len(content)
	}

	return nil
}
