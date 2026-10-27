package skills

import (
	"fmt"
	"time"
)

func validateBehavioralRunState(run BehavioralRun) error {
	if run.ID == "" || !isTrimExactUTF8(run.ID, 160) {
		return fmt.Errorf("%w: id", ErrBehavioralRunBindings)
	}
	if run.StartedAt.IsZero() || run.StartedAt.Location() != time.UTC {
		return fmt.Errorf("%w: started_at", ErrBehavioralRunBindings)
	}
	if run.CompletedAt != nil {
		if run.CompletedAt.IsZero() || run.CompletedAt.Location() != time.UTC || run.CompletedAt.Before(run.StartedAt) {
			return fmt.Errorf("%w: completed_at", ErrBehavioralRunBindings)
		}
	}
	allAbsent := run.EvaluationID == "" && run.ResultSHA256 == "" && run.CompletedAt == nil
	allPresent := run.EvaluationID != "" && isTrimExactUTF8(run.EvaluationID, 160) && isLowerHex64(run.ResultSHA256) && run.CompletedAt != nil
	switch run.State {
	case BehavioralRunPrepared, BehavioralRunDispatched, BehavioralRunUncertain:
		if !allAbsent {
			return fmt.Errorf("%w: state %s", ErrBehavioralRunBindings, run.State)
		}
	case BehavioralRunCompleted:
		if !allPresent {
			return fmt.Errorf("%w: state %s", ErrBehavioralRunBindings, run.State)
		}
	case BehavioralRunStale, BehavioralRunAborted:
		if !allAbsent && !allPresent {
			return fmt.Errorf("%w: state %s", ErrBehavioralRunBindings, run.State)
		}
	default:
		return fmt.Errorf("%w: unknown state %s", ErrBehavioralRunBindings, run.State)
	}
	return nil
}
