package taskcoord

import (
	"strings"
	"testing"
)

func TestProposalReviewFilesRejectsAfterMismatch(t *testing.T) {
	key := "<review-hidden-secret>"
	for _, mode := range []string{"hidden-after", "drift", "missing-after"} {
		f := newReviewProjectionFixture(t, "", "")
		f.profile.APIKeyEnv = "B4_HOST_REVIEW_KEY"
		t.Setenv("B4_HOST_REVIEW_KEY", key)
		if !f.service.providers.CredentialAppearsIn(f.profile, key) {
			t.Fatalf("credential not active in fixture")
		}
		switch mode {
		case "hidden-after":
			f.after["sum.go"] += "// hidden: " + key + "\n"
		case "drift":
			f.after["sum.go"] += "// owner edit\n"
		case "missing-after":
			delete(f.after, "sum.go")
		default:
			t.Fatalf("unknown mode %q", mode)
		}
		whole, partial, err := proposalReviewFiles(f.service, f.profile, &f.task, f.result, f.after)
		if err == nil {
			t.Fatalf("expected error for %s", mode)
		}
		if whole != nil || partial != nil {
			t.Fatalf("expected nil maps, got whole=%v partial=%v", whole, partial)
		}
		if f.adapter.calls != 1 {
			t.Fatalf("adapter.calls = %d, want 1", f.adapter.calls)
		}
		if f.adapter.reviewCalls != 0 {
			t.Fatalf("adapter.reviewCalls = %d, want 0", f.adapter.reviewCalls)
		}
		if mode == "hidden-after" {
			if !strings.Contains(err.Error(), "credential") {
				t.Fatalf("hidden-after error does not contain credential: %q", err.Error())
			}
		} else {
			if !strings.Contains(err.Error(), "differs") {
				t.Fatalf("%s error does not contain differs: %q", mode, err.Error())
			}
		}
	}
}
