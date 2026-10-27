package taskcoord

import (
	"hermetrix-harness/internal/inference"
	"hermetrix-harness/internal/worker"
)

// proposalTaskFits reports whether a task's estimated proposal input fits the preset budget.
// It is non-fatal: any estimation error returns false so oversized files can be curated.
func proposalTaskFits(task worker.Task, preset inference.Preset) bool {
	if preset.ContextMax <= 0 {
		return false
	}
	est, err := worker.EstimateProposalInput(task)
	if err != nil {
		return false
	}
	if est.WireBytes > 128*1024 || est.Tokens > preset.ContextMax {
		return false
	}
	return true
}
