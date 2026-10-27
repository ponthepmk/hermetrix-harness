package taskcoord

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestCodingOperationEnvelopeBoundaries(t *testing.T) {
	f := newCodingOperationFixture(t, &goalAdapter{})
	zero := CodingOperationInput{}
	var zeroRaw []byte
	zeroSHA := ""

	for _, tc := range []struct {
		name   string
		mutate func(in *CodingOperationInput)
	}{{
		name: "empty OperationID",
		mutate: func(in *CodingOperationInput) {
			in.OperationID = ""
		},
	}, {
		name: "leading whitespace OperationID",
		mutate: func(in *CodingOperationInput) {
			in.OperationID = "  coding-op-fixture"
		},
	}, {
		name: "OperationID too long",
		mutate: func(in *CodingOperationInput) {
			in.OperationID = strings.Repeat("x", 161)
		},
	}, {
		name: "caller Goal.GoalID nonempty",
		mutate: func(in *CodingOperationInput) {
			in.Goal.GoalID = "goal-123"
		},
	}, {
		name: "caller Goal.TaskID nonempty",
		mutate: func(in *CodingOperationInput) {
			in.Goal.TaskID = "task-123"
		},
	}, {
		name: "caller Goal.ExpectedTaskRevision non-zero",
		mutate: func(in *CodingOperationInput) {
			in.Goal.ExpectedTaskRevision = 1
		},
	}, {
		name: "ApplyChanges true with nil WritablePaths",
		mutate: func(in *CodingOperationInput) {
			in.Goal.ApplyChanges = true
			in.Task.Contract.WritablePaths = nil
		},
	}, {
		name: "Task.ProjectID leading whitespace",
		mutate: func(in *CodingOperationInput) {
			in.Task.ProjectID = "  " + f.input.Task.ProjectID
		},
	}, {
		name: "positive boundary 160 chars",
		mutate: func(in *CodingOperationInput) {
			in.OperationID = strings.Repeat("x", 160)
		},
	},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := f.input
			in.Task.Contract.WritablePaths = nil
			in.Goal.ApplyChanges = false
			in.Goal.GoalID = ""
			in.Goal.TaskID = ""
			in.Goal.ExpectedTaskRevision = 0
			in.Task.ProjectID = f.input.Task.ProjectID
			in.OperationID = f.input.OperationID
			tc.mutate(&in)

			if tc.name == "positive boundary 160 chars" {
				out, raw, sha, err := canonicalCodingInput(in)
				if err != nil {
					t.Fatalf("canonicalCodingInput: %v", err)
				}
				if len(raw) == 0 || sha == "" || out.OperationID != in.OperationID {
					t.Fatalf("expected nonempty raw and SHA, got raw=%d sha=%q", len(raw), sha)
				}
				if len(sha) != 64 {
					t.Fatalf("expected 64-char SHA, got %d", len(sha))
				}
				return
			}

			out, raw, sha, err := canonicalCodingInput(in)
			if err == nil {
				t.Fatalf("expected error, got none")
			}
			if !reflect.DeepEqual(out, zero) {
				t.Fatalf("expected zero input, got %+v", out)
			}
			if !reflect.DeepEqual(raw, zeroRaw) {
				t.Fatalf("expected nil raw, got %+v", raw)
			}
			if sha != zeroSHA {
				t.Fatalf("expected empty SHA, got %q", sha)
			}

			result, err := f.operations.Start(context.Background(), in)
			if err == nil {
				t.Fatalf("expected Start error, got none")
			}
			if !reflect.DeepEqual(result, CodingOperationResult{}) {
				t.Fatalf("expected zero result, got %+v", result)
			}

			assertCodingOperationCounts(t, f.data, 1, 0, 0)
			if f.adapter.plans != 0 || f.adapter.proposals != 0 || f.adapter.reviews != 0 {
				t.Fatalf("adapter.plans=%d, adapter.proposals=%d, adapter.reviews=%d, want all 0", f.adapter.plans, f.adapter.proposals, f.adapter.reviews)
			}
		})
	}
}
