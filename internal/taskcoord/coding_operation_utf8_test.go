package taskcoord

import (
	"reflect"
	"testing"

	"hermetrix-harness/internal/taskengine"
)

func TestCodingOperationInvalidUTF8(t *testing.T) {
	bad := string([]byte{0xff})
	zero := CodingOperationInput{}

	baseline := func() CodingOperationInput {
		return CodingOperationInput{
			OperationID: "op-1",
			Task: taskengine.CreateTaskInput{
				ProjectID: "project",
				Actor:     "owner",
			},
			Goal: StartGoalInput{},
		}
	}

	check := func(name string, mutate func(in *CodingOperationInput)) {
		in := baseline()
		mutate(&in)
		out, raw, sha, err := canonicalCodingInput(in)
		if err == nil {
			t.Fatalf("%s: expected error", name)
		}
		if !reflect.DeepEqual(out, zero) {
			t.Fatalf("%s: expected zero normalized input", name)
		}
		if raw != nil {
			t.Fatalf("%s: expected nil raw", name)
		}
		if sha != "" {
			t.Fatalf("%s: expected empty sha", name)
		}
	}

	check("OperationID", func(in *CodingOperationInput) {
		in.OperationID = bad
	})
	check("Task.Actor", func(in *CodingOperationInput) {
		in.Task.Actor = bad
	})
	check("Goal.Actor", func(in *CodingOperationInput) {
		in.Goal.Actor = bad
	})
	check("Task.Constraints[0]", func(in *CodingOperationInput) {
		in.Task.Constraints = []string{bad}
	})
	check("Task.Criteria[0].Description", func(in *CodingOperationInput) {
		in.Task.Criteria = []taskengine.Criterion{{Description: bad}}
	})
	check("Task.Contract.Clarifications[0].Answer", func(in *CodingOperationInput) {
		in.Task.Contract.Clarifications = []taskengine.GoalClarification{{Answer: bad}}
	})

	ok := CodingOperationInput{
		OperationID: "op-1",
		Task: taskengine.CreateTaskInput{
			ProjectID: "project",
			Actor:     "owner",
		},
		Goal: StartGoalInput{},
	}
	ok.OperationID = "งานทดสอบ"
	ok.Task.Actor = "ผู้ใช้"
	_, raw, sha, err := canonicalCodingInput(ok)
	if err != nil {
		t.Fatalf("valid Thai input: expected no error, got %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("valid Thai input: expected nonempty raw")
	}
	if len(sha) != 64 {
		t.Fatalf("valid Thai input: expected sha length 64, got %d", len(sha))
	}
}
