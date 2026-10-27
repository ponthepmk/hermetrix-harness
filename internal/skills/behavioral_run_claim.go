package skills

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
)

// ClaimBehavioralRun is a one-time durable intent to dispatch a prepared
// behavioral run. It does not execute the run, call a provider, or record
// any observed execution or provenance. It only transitions the run from
// prepared to dispatched when the caller presents the exact manifest SHA and
// the observed bindings that match the stored manifest.
func (s *Service) ClaimBehavioralRun(ctx context.Context, runID, expectedManifestSHA256 string, observedBindings BehavioralRunBindings) (BehavioralRun, error) {
	if runID == "" || !isTrimExactUTF8(runID, 160) {
		return BehavioralRun{}, fmt.Errorf("%w: run_id", ErrBehavioralRunBindings)
	}
	if expectedManifestSHA256 == "" || !isLowerHex64(expectedManifestSHA256) {
		return BehavioralRun{}, fmt.Errorf("%w: manifest_sha256", ErrBehavioralRunBindings)
	}
	if err := validateBehavioralRunBindings(observedBindings); err != nil {
		return BehavioralRun{}, err
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return BehavioralRun{}, err
	}
	defer func() {
		if tx != nil {
			tx.Rollback()
		}
	}()
	owner, err := ownerPrincipalInTx(ctx, tx)
	if err != nil {
		return BehavioralRun{}, err
	}
	run, err := scanBehavioralRun(tx.QueryRowContext(ctx, behavioralRunSelect+` WHERE id=? AND owner_principal_id=?`, runID, owner))
	if errors.Is(err, sql.ErrNoRows) {
		return BehavioralRun{}, ErrNotFound
	}
	if err != nil {
		return BehavioralRun{}, err
	}
	if run.ManifestSHA256 != expectedManifestSHA256 || !reflect.DeepEqual(run.Manifest.Bindings, observedBindings) {
		return BehavioralRun{}, fmt.Errorf("%w: manifest or bindings mismatch", ErrRevisionConflict)
	}
	switch run.State {
	case BehavioralRunPrepared:
		// ok
	case BehavioralRunDispatched, BehavioralRunUncertain:
		return BehavioralRun{}, ErrBehavioralRunUncertain
	default:
		return BehavioralRun{}, fmt.Errorf("%w: state %q", ErrRevisionConflict, run.State)
	}
	if err := validateBehavioralRunCurrent(ctx, tx, run, owner); err != nil {
		return BehavioralRun{}, err
	}
	// This is a one-time durable intent before effect, not observed execution or provenance.
	newRun := run
	newRun.State = BehavioralRunDispatched
	res, err := tx.ExecContext(ctx, `UPDATE behavioral_eval_runs SET state=? WHERE id=? AND owner_principal_id=? AND manifest_sha256=? AND state=? AND evaluation_id IS NULL AND result_sha256='' AND completed_at IS NULL`,
		BehavioralRunDispatched, runID, owner, expectedManifestSHA256, BehavioralRunPrepared)
	if err != nil {
		return BehavioralRun{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return BehavioralRun{}, err
	}
	if n != 1 {
		return BehavioralRun{}, fmt.Errorf("%w: expected one row", ErrRevisionConflict)
	}
	if err := tx.Commit(); err != nil {
		return BehavioralRun{}, err
	}
	return newRun, nil
}
