package taskcoord

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
)

func TestCodingOperationChangedProfileBeforeGoal(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "disable_writer"},
		{name: "disable_reviewer"},
		{name: "change_writer_model"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, op := newPendingCodingOperationFixture(t)
			ctx := context.Background()

			var profileID string
			if tt.name == "disable_reviewer" {
				profileID = op.GoalConfig.ReviewerProviderID
			} else {
				profileID = op.GoalConfig.ProviderID
			}

			var err error
			var res sql.Result
			if tt.name == "change_writer_model" {
				res, err = f.data.DB.ExecContext(ctx, "UPDATE provider_profiles SET model = model || '-changed' WHERE id = ?", profileID)
				if err != nil {
					t.Fatalf("update model: %v", err)
				}
				affected, rowsErr := res.RowsAffected()
				if rowsErr != nil || affected != 1 {
					t.Fatalf("expected exactly 1 row affected, got %d (err=%v)", affected, rowsErr)
				}
			} else {
				res, err = f.data.DB.ExecContext(ctx, "UPDATE provider_profiles SET enabled = 0 WHERE id = ?", profileID)
				if err != nil {
					t.Fatalf("disable profile: %v", err)
				}
				affected, rowsErr := res.RowsAffected()
				if rowsErr != nil || affected != 1 {
					t.Fatalf("expected exactly 1 row affected, got %d (err=%v)", affected, rowsErr)
				}
			}
			if err != nil {
				t.Fatalf("update profile: %v", err)
			}

			startResult, startErr := f.operations.Start(ctx, f.input)
			if startErr == nil {
				t.Fatalf("Start returned nil error")
			}
			if !reflect.DeepEqual(startResult, CodingOperationResult{}) {
				t.Fatalf("Start result = %+v, want zero", startResult)
			}

			advResult, advErr := f.operations.Advance(ctx, op.ID)
			if advErr == nil {
				t.Fatalf("Advance returned nil error")
			}
			if !reflect.DeepEqual(advResult, CodingOperationResult{}) {
				t.Fatalf("Advance result = %+v, want zero", advResult)
			}

			pending, err := f.operations.Get(ctx, op.ID)
			if err != nil {
				t.Fatalf("GetPending: %v", err)
			}
			if pending.Goal != nil {
				t.Fatalf("pending.Goal = %+v, want nil", pending.Goal)
			}
			if !reflect.DeepEqual(pending.Operation, op) {
				t.Fatalf("pending.Operation = %+v, want original op %+v", pending.Operation, op)
			}

			assertCodingOperationCounts(t, f.data, 2, 1, 0)
			if f.adapter.plans != 0 || f.adapter.proposals != 0 || f.adapter.reviews != 0 {
				t.Fatalf("expected zero counters, got plans=%d proposals=%d reviews=%d", f.adapter.plans, f.adapter.proposals, f.adapter.reviews)
			}
		})
	}
}
