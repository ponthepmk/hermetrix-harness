package worker

import (
	"context"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestOutputLimitReportsTruncatedProposal(t *testing.T) {
	err := providerError(context.Background(), &providers.IncompleteCompletionError{
		Reason: "tool call 0 has incomplete arguments",
		Partial: providers.Completion{FinishReason: "length", Content: "partial code"},
	})
	workerErr, ok := err.(*Error)
	if !ok || workerErr.Code != ErrorResponseTruncated || !workerErr.Retryable || workerErr.Message != "provider response reached its output limit before the proposal was complete" {
		t.Fatalf("unexpected worker error: %#v", err)
	}
}
