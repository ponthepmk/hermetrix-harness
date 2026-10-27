package worker

import "errors"

const (
	maxWorkerFileBytes      = 2 * 1024 * 1024
	maxWorkerSourceBytes    = 8 * 1024 * 1024
	maxWorkerCanonicalBytes = 16 * 1024 * 1024
	maxWorkerWireBytes      = 128 * 1024
)

// validateWorkerTaskResources checks bounded task resource limits without IO.
func validateWorkerTaskResources(task Task) error {
	if err := ValidateTaskSourceResources(task); err != nil {
		return err
	}
	if len(task.FileViews) > 32 {
		return errors.New("task has more than 32 file views")
	}
	for _, views := range task.FileViews {
		if len(views) < 1 || len(views) > 32 {
			return errors.New("file view must contain 1 to 32 views")
		}
	}

	return validateWorkerWireLowerBound(task)
}
