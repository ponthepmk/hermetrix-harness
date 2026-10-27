package taskcoord

import (
	"context"
	"reflect"
	"sync"
	"testing"
)

func TestCodingOperationConcurrentStart(t *testing.T) {
	f := newCodingOperationFixture(t, &goalAdapter{})
	ctx := context.Background()

	var wg sync.WaitGroup
	var barrier = make(chan struct{})
	var results [4]CodingOperationResult
	var errs [4]error

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-barrier
			res, err := f.operations.Start(ctx, f.input)
			results[i] = res
			errs[i] = err
		}(i)
	}

	close(barrier)
	wg.Wait()

	for i := 0; i < 4; i++ {
		if errs[i] != nil {
			t.Fatalf("start %d: %v", i, errs[i])
		}
		if results[i].Goal == nil {
			t.Fatalf("start %d: goal is nil", i)
		}
		if results[i].Operation.ID != f.input.OperationID {
			t.Fatalf("start %d: operation id mismatch", i)
		}
		if results[i].Task.ID != results[i].Operation.TaskID {
			t.Fatalf("start %d: task id mismatch", i)
		}
		if results[i].Operation.TaskID != results[i].Goal.TaskID {
			t.Fatalf("start %d: goal task id mismatch", i)
		}
		if results[i].Goal.ID != results[i].Operation.GoalID {
			t.Fatalf("start %d: goal id mismatch", i)
		}
		if !reflect.DeepEqual(results[0], results[i]) {
			t.Fatalf("start %d: result differs from first", i)
		}
	}

	assertCodingOperationCounts(t, f.data, 2, 1, 1)

	if f.adapter.plans != 0 || f.adapter.proposals != 0 || f.adapter.reviews != 0 {
		t.Fatalf("adapter counts: plans=%d proposals=%d reviews=%d", f.adapter.plans, f.adapter.proposals, f.adapter.reviews)
	}

	res, err := f.operations.Start(ctx, f.input)
	if err != nil {
		t.Fatalf("retry start: %v", err)
	}
	if !reflect.DeepEqual(res, results[0]) {
		t.Fatalf("retry result differs")
	}

	assertCodingOperationCounts(t, f.data, 2, 1, 1)
}
