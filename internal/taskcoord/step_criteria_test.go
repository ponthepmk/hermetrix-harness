package taskcoord

import (
	"hermetrix-harness/internal/taskengine"
	"reflect"
	"testing"
)

func TestStepCriteriaDoesNotDemandFutureSteps(t *testing.T) {
	p := taskengine.StepPacket{}
	p.Requirement.Criteria = []taskengine.Criterion{{ID: "AC-1", Description: "auth"}, {ID: "AC-2", Description: "delivery"}}
	p.Step.RequirementIDs = []string{"AC-1"}
	if got := stepCriteria(p); !reflect.DeepEqual(got, []string{"AC-1: auth"}) {
		t.Fatalf("current worker got future work: %v", got)
	}
	p.Step.RequirementIDs = nil
	if len(stepCriteria(p)) != 2 {
		t.Fatal("legacy unscoped step lost acceptance criteria")
	}
}
