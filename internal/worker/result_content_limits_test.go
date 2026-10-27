package worker

import (
	"strings"
	"testing"
)

func resultResourceFixture() Result {
	hash := strings.Repeat("a", 64)
	return Result{
		Status:      "proposed_unverified",
		TaskID:      "r",
		ProposalID:  hash,
		InputSHA256: hash,
		Changes: []Change{{
			Path:         "x",
			BeforeSHA256: hash,
			Content:      "new",
			Strategy:     "replace_text",
		}},
	}
}

func TestValidateWorkerResultResourcesContentLimits(t *testing.T) {
	result := resultResourceFixture()
	result.Changes[0].Content = strings.Repeat("a", 2*1024*1024)
	if err := validateWorkerResultResources(result); err != nil {
		t.Fatalf("2 MiB content should be accepted, got %v", err)
	}
	result.Changes[0].Content += "a"
	if err := validateWorkerResultResources(result); err == nil {
		t.Fatal("2 MiB + 1 byte content should be rejected")
	}
}
