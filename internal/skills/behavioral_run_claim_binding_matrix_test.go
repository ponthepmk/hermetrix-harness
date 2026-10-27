package skills

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

type claimBindingCaseFile struct {
	BaseBindings     BehavioralRunBindings `json:"base_bindings"`
	ManifestSHA256   string                `json:"manifest_sha_mismatch"`
	BindingCaseCount int16                 `json:"binding_case_count"`
	BindingCases     []claimBindingCase    `json:"binding_cases"`
}

type claimBindingCase struct {
	Name          string                `json:"name"`
	Bindings      BehavioralRunBindings `json:"bindings"`
	ExpectedError string                `json:"expected_error"`
}

func TestBehavioralRunClaimRejectsChangedBindings(t *testing.T) {
	ctx := context.Background()
	data, err := os.ReadFile("behavioral_run_claim_binding_cases.json")
	if err != nil {
		t.Fatalf("read cases: %v", err)
	}
	var file claimBindingCaseFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("unmarshal cases: %v", err)
	}
	if file.BindingCaseCount != 16 {
		t.Fatalf("binding case count = %d, want 16", file.BindingCaseCount)
	}
	if len(file.BindingCases) != 16 {
		t.Fatalf("binding cases length = %d, want 16", len(file.BindingCases))
	}
	for _, c := range file.BindingCases {
		if c.Name == "" {
			t.Fatalf("case name empty")
		}
		if reflect.DeepEqual(c.Bindings, file.BaseBindings) {
			t.Fatalf("case %q bindings equal base", c.Name)
		}
		if c.ExpectedError != "revision_conflict" {
			t.Fatalf("case %q expected_error = %q, want revision_conflict", c.Name, c.ExpectedError)
		}
	}

	svc, ds := testService(t)
	defer ds.Close()
	in := candidateInput("claim-binding-matrix", "matrix", "x")
	cand, err := svc.CreateCandidate(ctx, in)
	if err != nil {
		t.Fatalf("create candidate: %v", err)
	}
	run, err := svc.PrepareBehavioralRun(ctx, "run-claim-binding-matrix", cand.ID, file.BaseBindings)
	if err != nil {
		t.Fatalf("prepare run: %v", err)
	}
	if run.State != BehavioralRunPrepared {
		t.Fatalf("prepared run state = %q, want prepared", run.State)
	}
	if run.ManifestSHA256 == "" {
		t.Fatalf("prepared run manifest sha empty")
	}
	if !reflect.DeepEqual(run.Manifest.Bindings, file.BaseBindings) {
		t.Fatalf("prepared manifest bindings do not match base")
	}

	for _, c := range file.BindingCases {
		got, err := svc.ClaimBehavioralRun(ctx, run.ID, run.ManifestSHA256, c.Bindings)
		if !errors.Is(err, ErrRevisionConflict) {
			t.Fatalf("case %q: claim error = %v, want revision_conflict", c.Name, err)
		}
		if !reflect.DeepEqual(got, (BehavioralRun{})) {
			t.Fatalf("case %q: returned run not zero", c.Name)
		}
		cur, err := svc.GetBehavioralRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("case %q: get run: %v", c.Name, err)
		}
		if !reflect.DeepEqual(cur, run) {
			t.Fatalf("case %q: run changed after rejected claim", c.Name)
		}
	}

	got, err := svc.ClaimBehavioralRun(ctx, run.ID, file.ManifestSHA256, file.BaseBindings)
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("manifest sha mismatch: claim error = %v, want revision_conflict", err)
	}
	if !reflect.DeepEqual(got, (BehavioralRun{})) {
		t.Fatalf("manifest sha mismatch: returned run not zero")
	}
	cur, err := svc.GetBehavioralRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("manifest sha mismatch: get run: %v", err)
	}
	if !reflect.DeepEqual(cur, run) {
		t.Fatalf("manifest sha mismatch: run changed")
	}

	got, err = svc.ClaimBehavioralRun(ctx, run.ID, run.ManifestSHA256, file.BaseBindings)
	if err != nil {
		t.Fatalf("original claim: %v", err)
	}
	if got.State != BehavioralRunDispatched {
		t.Fatalf("original claim state = %q, want dispatched", got.State)
	}
	if got.ManifestSHA256 != run.ManifestSHA256 {
		t.Fatalf("original claim manifest sha changed")
	}
	if !reflect.DeepEqual(got.Manifest.Bindings, file.BaseBindings) {
		t.Fatalf("original claim manifest bindings changed")
	}

	var runs int64
	if err := ds.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM behavioral_eval_runs`).Scan(&runs); err != nil {
		t.Fatalf("count behavioral_eval_runs: %v", err)
	}
	if runs != 1 {
		t.Fatalf("behavioral_eval_runs count = %d, want 1", runs)
	}
	var candEvals int64
	if err := ds.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM candidate_behavioral_evals`).Scan(&candEvals); err != nil {
		t.Fatalf("count candidate_behavioral_evals: %v", err)
	}
	if candEvals != 0 {
		t.Fatalf("candidate_behavioral_evals count = %d, want 0", candEvals)
	}
}
