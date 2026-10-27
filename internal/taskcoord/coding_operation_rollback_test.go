package taskcoord

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"hermetrix-harness/internal/taskengine"
)

func TestCodingOperationAtomicRollback(t *testing.T) {
	for _, caseName := range []string{"ABORT", "IGNORE"} {
		t.Run(caseName, func(t *testing.T) {
			f := newCodingOperationFixture(t, &goalAdapter{})
			ctx := context.Background()
			var triggerSQL string
			switch caseName {
			case "ABORT":
				triggerSQL = `CREATE TRIGGER injected_host_failure BEFORE INSERT ON coding_goal_operations BEGIN SELECT RAISE(ABORT, 'injected host failure'); END;`
			case "IGNORE":
				triggerSQL = `CREATE TRIGGER injected_host_failure BEFORE INSERT ON coding_goal_operations BEGIN SELECT RAISE(IGNORE); END;`
			default:
				t.Fatalf("unknown case %q", caseName)
			}
			if _, err := f.data.DB.Exec(triggerSQL); err != nil {
				t.Fatalf("create trigger: %v", err)
			}
			defer func() {
				if _, err := f.data.DB.Exec(`DROP TRIGGER IF EXISTS injected_host_failure`); err != nil {
					t.Logf("drop trigger injected_host_failure: %v", err)
				}
			}()
			assertCodingOperationCounts(t, f.data, 1, 0, 0)
			result, err := f.operations.Start(ctx, f.input)
			if err == nil {
				t.Fatalf("Start succeeded, want error")
			}
			if errors.Is(err, taskengine.ErrNotFound) {
				t.Fatalf("Start error is ErrNotFound: %v", err)
			}
			if caseName == "ABORT" && !strings.Contains(err.Error(), "injected host failure") {
				t.Fatalf("ABORT error missing injected host failure: %v", err)
			}
			zero := CodingOperationResult{}
			if !reflect.DeepEqual(result, zero) {
				t.Fatalf("Start result not zero: %+v", result)
			}
			assertCodingOperationCounts(t, f.data, 1, 0, 0)
			if _, err := f.data.DB.Exec(`DROP TRIGGER IF EXISTS injected_host_failure`); err != nil {
				t.Fatalf("drop trigger injected_host_failure: %v", err)
			}
			result, err = f.operations.Start(ctx, f.input)
			if err != nil {
				t.Fatalf("retry Start: %v", err)
			}
			if result.Goal == nil {
				t.Fatalf("retry goal nil")
			}
			assertCodingOperationCounts(t, f.data, 2, 1, 1)
			if f.adapter.plans != 0 || f.adapter.proposals != 0 || f.adapter.reviews != 0 {
				t.Fatalf("adapter counts not zero after successful retry: plans=%d proposals=%d reviews=%d", f.adapter.plans, f.adapter.proposals, f.adapter.reviews)
			}
		})
	}
}
