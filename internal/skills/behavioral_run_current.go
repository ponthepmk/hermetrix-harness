package skills

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func validateBehavioralRunCurrent(ctx context.Context, tx *sql.Tx, run BehavioralRun, ownerID string) error {
	if ownerID == "" {
		return fmt.Errorf("%w: ownerID must be nonempty", ErrBehavioralRunBindings)
	}
	if run.Manifest.OwnerPrincipalID == "" || run.Manifest.OwnerPrincipalID != ownerID {
		return fmt.Errorf("%w: owner mismatch", ErrBehavioralRunBindings)
	}
	candidate, err := scanCandidate(tx.QueryRowContext(ctx, candidateSelect+" WHERE id=? AND owner_principal_id=?", run.Manifest.CandidateID, ownerID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: candidate not found", ErrNotFound)
		}
		return err
	}
	if candidate.State != CandidateNeedsReview {
		return fmt.Errorf("%w: candidate not ready", ErrCandidateNotReady)
	}
	if !candidate.Checks.Passed {
		return fmt.Errorf("%w: checks failed", ErrChecksFailed)
	}
	if candidate.ID != run.Manifest.CandidateID ||
		candidate.Revision != run.Manifest.CandidateRevision ||
		candidate.CandidateHash != run.Manifest.CandidateHash ||
		candidate.CandidateBlobRef != run.Manifest.CandidateBlobRef ||
		candidate.BaseVersionID != run.Manifest.BaseVersionID ||
		candidate.ScopeKind != run.Manifest.ScopeKind ||
		candidate.ScopeRef != run.Manifest.ScopeRef {
		return fmt.Errorf("%w: candidate mismatch", ErrRevisionConflict)
	}
	if err := validateBehavioralRunScope(ctx, tx, candidate, run.Manifest.Bindings, ownerID); err != nil {
		return err
	}
	baseHash, baseBlob, err := behavioralRunBaseSnapshot(ctx, tx, candidate, ownerID)
	if err != nil {
		return err
	}
	if baseHash != run.Manifest.BaseHash || baseBlob != run.Manifest.BaseBlobRef {
		return fmt.Errorf("%w: base mismatch", ErrRevisionConflict)
	}
	return nil
}
