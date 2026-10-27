package worker

import (
	"strings"
	"testing"
)

func TestResultMetadataLimitsIndividualListControls(t *testing.T) {
	for _, field := range []string{"assumptions", "risks", "checks"} {
		for _, count := range []int{32, 33} {
			r := resultResourceFixture()
			switch field {
			case "assumptions":
				r.Assumptions = make([]string, count)
			case "risks":
				r.Risks = make([]string, count)
			case "checks":
				r.RecommendedChecks = make([]string, count)
			}
			got := validateWorkerResultResources(r)
			wantErr := count > 32
			if got != nil != wantErr {
				t.Fatalf("field=%s count=%d: error presence mismatch", field, count)
			}
		}
	}
}

func TestResultMetadataLimitsCombinedBudgetAndContentExemption(t *testing.T) {
	r1 := resultResourceFixture()
	r1.Summary = strings.Repeat("a", 200*1024)
	if err := validateWorkerResultResources(r1); err != nil {
		t.Fatalf("Summary200KiB should pass: %v", err)
	}

	r2 := resultResourceFixture()
	r2.Summary = strings.Repeat("a", 257*1024)
	if err := validateWorkerResultResources(r2); err == nil {
		t.Fatalf("Summary257KiB should fail")
	}

	r3 := resultResourceFixture()
	r3.Summary = strings.Repeat("a", 130*1024)
	r3.Assumptions = []string{strings.Repeat("b", 130*1024)}
	if err := validateWorkerResultResources(r3); err == nil {
		t.Fatalf("Summary130KiB plus Assumption130KiB should fail")
	}

	r4 := resultResourceFixture()
	r4.Summary = strings.Repeat("a", 200*1024)
	r4.Changes = []Change{{Content: strings.Repeat("c", 2*1024*1024)}}
	if err := validateWorkerResultResources(r4); err != nil {
		t.Fatalf("Content2MiB plus Summary200KiB should pass: %v", err)
	}
}
