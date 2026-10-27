package worker

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestPartialReviewRejectsInvalidUTF8BeforeDispatch(t *testing.T) {
	service, profile, capture := newPartialReviewService(t)

	data, err := os.ReadFile("review_input_fixture.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	for _, before := range []bool{true, false} {
		var task ReviewTask
		if err := json.Unmarshal(data, &task); err != nil {
			t.Fatalf("unmarshal fixture: %v", err)
		}

		if before {
			task.PartialFiles["sum.go"].Regions[0].Before.Content = strings.Repeat(string([]byte{0xff}), 10)
		} else {
			task.PartialFiles["sum.go"].Regions[0].AfterContent = string([]byte{0xff})
		}

		ctx := context.Background()
		result, err := ReviewWithProviderService(ctx, service, profile, task, Options{
			BeforeProviderRequest: func() error {
				capture.Auth++
				return nil
			},
		})
		if err == nil {
			t.Fatalf("expected error for invalid UTF-8, got none")
		}
		if !reflect.DeepEqual(result, ReviewResult{}) {
			t.Fatalf("expected empty ReviewResult, got %v", result)
		}
		if capture.Calls != 0 || capture.Auth != 0 {
			t.Fatalf("expected no provider calls/auth, got calls=%d auth=%d", capture.Calls, capture.Auth)
		}
	}
}
