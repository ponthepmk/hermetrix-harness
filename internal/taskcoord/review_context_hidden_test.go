package taskcoord

import (
	"strings"
	"testing"
)

func TestProposalReviewFilesRejectsHiddenKeys(t *testing.T) {
	key := "<review-hidden-secret>"
	for _, readonly := range []bool{false, true} {
		mode := "before"
		if readonly {
			mode = "readonly"
		}
		hidden := ""
		readonlyStr := ""
		if mode == "before" {
			hidden = key
		} else {
			readonlyStr = key
		}
		f := newReviewProjectionFixture(t, hidden, readonlyStr)
		t.Setenv("B4_HOST_REVIEW_KEY", key)
		f.profile.APIKeyEnv = "B4_HOST_REVIEW_KEY"
		if !f.service.providers.CredentialAppearsIn(f.profile, key) {
			t.Fatalf("credential not in profile")
		}
		whole, partial, err := proposalReviewFiles(f.service, f.profile, &f.task, f.result, f.after)
		if err == nil {
			t.Fatalf("expected error")
		}
		if !strings.Contains(err.Error(), "credential") {
			t.Fatalf("error %q does not contain credential", err)
		}
		if whole != nil || partial != nil {
			t.Fatalf("maps not nil")
		}
		if f.adapter.calls != 1 || f.adapter.reviewCalls != 0 {
			t.Fatalf("calls=%d reviewCalls=%d", f.adapter.calls, f.adapter.reviewCalls)
		}
	}
}
