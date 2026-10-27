package skills

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"time"
)

func (s *Service) PrepareBehavioralRun(ctx context.Context, runID, candidateID string, bindings BehavioralRunBindings) (BehavioralRun, error) {
	if !isTrimExactUTF8(runID, 160) || runID == "" {
		return BehavioralRun{}, fmt.Errorf("%w: run_id", ErrBehavioralRunBindings)
	}
	if !isTrimExactUTF8(candidateID, 160) || candidateID == "" {
		return BehavioralRun{}, fmt.Errorf("%w: candidate_id", ErrBehavioralRunBindings)
	}
	if err := validateBehavioralRunBindings(bindings); err != nil {
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
	existing, err := scanBehavioralRun(tx.QueryRowContext(ctx, behavioralRunSelect+" WHERE id=? AND owner_principal_id=?", runID, owner))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// new run
		} else {
			return BehavioralRun{}, err
		}
	} else {
		if existing.Manifest.CandidateID != candidateID || !reflect.DeepEqual(bindings, existing.Manifest.Bindings) {
			return BehavioralRun{}, fmt.Errorf("%w: revision conflict", ErrRevisionConflict)
		}
		return existing, nil
	}
	c, err := scanCandidate(tx.QueryRowContext(ctx, candidateSelect+" WHERE id=? AND owner_principal_id=?", candidateID, owner))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BehavioralRun{}, fmt.Errorf("%w: candidate not found", ErrNotFound)
		}
		return BehavioralRun{}, err
	}
	if c.State != CandidateNeedsReview {
		return BehavioralRun{}, fmt.Errorf("%w: candidate not ready", ErrCandidateNotReady)
	}
	if !c.Checks.Passed {
		return BehavioralRun{}, fmt.Errorf("%w: checks failed", ErrChecksFailed)
	}
	if err := validateBehavioralRunScope(ctx, tx, c, bindings, owner); err != nil {
		return BehavioralRun{}, err
	}
	baseHash, baseBlob, err := behavioralRunBaseSnapshot(ctx, tx, c, owner)
	if err != nil {
		return BehavioralRun{}, err
	}
	caseIDs := make([]string, len(bindings.CaseIDs))
	copy(caseIDs, bindings.CaseIDs)
	bindings.CaseIDs = caseIDs
	m := BehavioralRunManifest{
		CandidateID:       candidateID,
		CandidateHash:     c.CandidateHash,
		CandidateBlobRef:  c.CandidateBlobRef,
		CandidateRevision: c.Revision,
		BaseVersionID:     c.BaseVersionID,
		BaseHash:          baseHash,
		BaseBlobRef:       baseBlob,
		OwnerPrincipalID:  owner,
		ScopeKind:         c.ScopeKind,
		ScopeRef:          c.ScopeRef,
		Bindings:          bindings,
	}
	encoded, sha, err := behavioralManifestJSON(m)
	if err != nil {
		return BehavioralRun{}, err
	}
	_ = encoded
	run := BehavioralRun{
		ID:             runID,
		Manifest:       m,
		ManifestSHA256: sha,
		State:          BehavioralRunPrepared,
		StartedAt:      time.Now().UTC(),
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO behavioral_eval_runs (id, owner_principal_id, candidate_id, candidate_revision, candidate_hash, manifest_json, manifest_sha256, started_at) VALUES (?,?,?,?,?,?,?,?)`,
		run.ID, owner, candidateID, c.Revision, c.CandidateHash, string(encoded), sha, formatTime(run.StartedAt))
	if err != nil {
		return BehavioralRun{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return BehavioralRun{}, err
	}
	if n != 1 {
		return BehavioralRun{}, fmt.Errorf("%w: expected one row", ErrBehavioralRunBindings)
	}
	if err := tx.Commit(); err != nil {
		return BehavioralRun{}, err
	}
	return run, nil
}
