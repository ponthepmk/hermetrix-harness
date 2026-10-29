package worker

import (
	"encoding/json"
	"errors"
	"fmt"
)

// maxWorkerRawProposalBytes bounds the raw model-supplied proposal reply.
const maxWorkerRawProposalBytes = 256 * 1024

// validateWorkerResultResources performs bounded size/count preflight for a
// generated or persisted Result before expensive identity hashing. It makes
// no permission or correctness claims and performs no IO.
func validateWorkerResultResources(result Result) error {
	if len(result.Changes) < 1 || len(result.Changes) > 32 {
		return errors.New("result must contain 1 to 32 changes")
	}

	remainingSource := maxWorkerSourceBytes + maxWorkerRawProposalBytes
	for _, change := range result.Changes {
		if len(change.Content) > maxWorkerFileBytes {
			return errors.New("change content exceeds 2 MiB")
		}
		if len(change.Content) > remainingSource {
			return errors.New("aggregate change content exceeds 8 MiB plus 256 KiB")
		}
		remainingSource -= len(change.Content)
	}

	remainingRaw := maxWorkerRawProposalBytes
	if err := consumeRawBudget(&remainingRaw, result.Summary); err != nil {
		return err
	}
	if err := consumeRawBudget(&remainingRaw, result.Assumptions); err != nil {
		return err
	}
	if err := consumeRawBudget(&remainingRaw, result.Risks); err != nil {
		return err
	}
	if err := consumeRawBudget(&remainingRaw, result.RecommendedChecks); err != nil {
		return err
	}
	for _, change := range result.Changes {
		if err := consumeRawBudget(&remainingRaw, change.Path); err != nil {
			return err
		}
		if err := consumeRawBudget(&remainingRaw, change.Edits); err != nil {
			return err
		}
		if change.Repair != nil {
			if len(change.Repair.SourceSHA256) != 64 {
				return errors.New("repair proof source sha256 must contain 64 bytes")
			}
			if err := consumeRawBudget(&remainingRaw, change.Repair.SourceSHA256); err != nil {
				return err
			}
			if err := consumeRawBudget(&remainingRaw, change.Repair.Edits); err != nil {
				return err
			}
			if err := consumeRawBudget(&remainingRaw, change.Repair.TargetEdits); err != nil {
				return err
			}
		}
	}

	if len(result.TaskID) > maxWorkerWireBytes {
		return errors.New("task id exceeds 128 KiB")
	}
	if len(result.Status) > 64 {
		return errors.New("status exceeds 64 bytes")
	}
	if len(result.InputSHA256) > 64 {
		return errors.New("input sha256 exceeds 64 bytes")
	}
	if len(result.ProposalID) > 64 {
		return errors.New("proposal id exceeds 64 bytes")
	}
	for _, change := range result.Changes {
		if len(change.BeforeSHA256) > 64 {
			return errors.New("before sha256 exceeds 64 bytes")
		}
		if len(change.Strategy) > 32 {
			return errors.New("strategy exceeds 32 bytes")
		}
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		return errors.New("result is not JSON serializable")
	}
	if len(encoded) > maxWorkerCanonicalBytes {
		return errors.New("encoded result exceeds 16 MiB")
	}
	return nil
}

// consumeRawBudget consumes the byte length of a single raw model-supplied
// value from the remaining raw proposal budget. It checks each length before
// subtracting to avoid overflow-prone combined sums.
func consumeRawBudget(remaining *int, value any) error {
	switch v := value.(type) {
	case string:
		if len(v) > *remaining {
			return errors.New("raw proposal metadata exceeds 256 KiB")
		}
		*remaining -= len(v)
	case []string:
		if len(v) > 32 {
			return errors.New("assumptions list exceeds 32 items")
		}
		if len(v) > *remaining {
			return errors.New("raw proposal metadata exceeds 256 KiB")
		}
		*remaining -= len(v)
		for _, item := range v {
			if len(item) > *remaining {
				return errors.New("raw proposal metadata exceeds 256 KiB")
			}
			*remaining -= len(item)
		}
	case []TextEdit:
		if len(v) > 64 {
			return errors.New("edits list exceeds 64 items")
		}
		if len(v) > *remaining {
			return errors.New("raw proposal metadata exceeds 256 KiB")
		}
		*remaining -= len(v)
		for _, edit := range v {
			if len(edit.Old) > *remaining {
				return errors.New("raw proposal metadata exceeds 256 KiB")
			}
			*remaining -= len(edit.Old)
			if len(edit.New) > *remaining {
				return errors.New("raw proposal metadata exceeds 256 KiB")
			}
			*remaining -= len(edit.New)
		}
	case []DraftTargetEdit:
		if len(v) > maxRepairTargets {
			return errors.New("too many named target edits")
		}
		for _, edit := range v {
			if len(edit.TargetID) != 64 {
				return errors.New("target ID must contain 64 bytes")
			}
			if err := consumeRawBudget(remaining, edit.TargetID); err != nil {
				return err
			}
			if err := consumeRawBudget(remaining, edit.New); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported raw proposal metadata type %T", value)
	}
	return nil
}
