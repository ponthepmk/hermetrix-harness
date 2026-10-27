// Package worker provides a proposal-only external model boundary. It never
// reads a workspace, executes commands, or applies model-generated changes.
package worker

// FreezeTask delegates directly to prepareWorkerTask and returns all four
// values unchanged: the deep task snapshot, the canonical full task JSON,
// the bounded provider wire JSON, and any error.
func FreezeTask(task Task) (Task, []byte, []byte, error) {
	return prepareWorkerTask(task)
}
