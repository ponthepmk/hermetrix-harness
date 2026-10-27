package skills

import (
	"database/sql"
	"fmt"
)

const behavioralRunSelect = `SELECT id, owner_principal_id, candidate_id, candidate_revision, candidate_hash, manifest_json, manifest_sha256, state, COALESCE(evaluation_id, ''), result_sha256, started_at, completed_at FROM behavioral_eval_runs`

func scanBehavioralRun(row scanner) (BehavioralRun, error) {
	run := BehavioralRun{}
	var (
		LOCALowner             string
		LOCALcandidateID       string
		LOCALcandidateRevision int
		LOCALcandidateHash     string
		LOCALmanifestBody      string
		LOCALstarted           string
		LOCALcompleted         sql.NullString
	)
	if err := row.Scan(
		&run.ID,
		&LOCALowner,
		&LOCALcandidateID,
		&LOCALcandidateRevision,
		&LOCALcandidateHash,
		&LOCALmanifestBody,
		&run.ManifestSHA256,
		&run.State,
		&run.EvaluationID,
		&run.ResultSHA256,
		&LOCALstarted,
		&LOCALcompleted,
	); err != nil {
		return BehavioralRun{}, err
	}
	manifest, err := decodeBehavioralRunManifest(LOCALmanifestBody, run.ManifestSHA256)
	if err != nil {
		return BehavioralRun{}, err
	}
	run.Manifest = manifest
	if LOCALowner != manifest.OwnerPrincipalID || LOCALcandidateID != manifest.CandidateID || LOCALcandidateRevision != manifest.CandidateRevision || LOCALcandidateHash != manifest.CandidateHash {
		return BehavioralRun{}, fmt.Errorf("%w: denormalized candidate/owner mismatch", ErrBehavioralRunBindings)
	}
	started, err := parseTime(LOCALstarted)
	if err != nil || formatTime(started) != LOCALstarted {
		return BehavioralRun{}, fmt.Errorf("%w: started_at", ErrBehavioralRunBindings)
	}
	run.StartedAt = started
	if LOCALcompleted.Valid {
		completed, err := parseTime(LOCALcompleted.String)
		if err != nil || formatTime(completed) != LOCALcompleted.String {
			return BehavioralRun{}, fmt.Errorf("%w: completed_at", ErrBehavioralRunBindings)
		}
		run.CompletedAt = &completed
	}
	if err := validateBehavioralRunState(run); err != nil {
		return BehavioralRun{}, err
	}
	return run, nil
}
