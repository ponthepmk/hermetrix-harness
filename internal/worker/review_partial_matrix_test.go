package worker

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

type partialReviewCase struct {
	Name      string     `json:"name"`
	WantError bool       `json:"want_error"`
	Task      ReviewTask `json:"task"`
}

func TestPartialReviewEndpointFrozenCases(t *testing.T) {
	service, profile, capture := newPartialReviewService(t)

	data, err := os.ReadFile("review_input_cases.json")
	if err != nil {
		t.Fatalf("read review_input_cases.json: %v", err)
	}
	var cases []partialReviewCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("unmarshal review_input_cases.json: %v", err)
	}
	if len(cases) != 38 {
		t.Fatalf("expected 38 rows, got %d", len(cases))
	}

	positive := 0
	negative := 0
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			saveCalls := capture.Calls
			saveAuth := capture.Auth
			options := Options{
				BeforeProviderRequest: func() error {
					capture.Auth++
					return nil
				},
			}
			result, err := ReviewWithProviderService(context.Background(), service, profile, tc.Task, options)
			if err != nil {
				if !tc.WantError {
					t.Fatalf("expected success for %s, got error: %v", tc.Name, err)
				}
				if !reflect.DeepEqual(result, ReviewResult{}) {
					t.Fatalf("expected zero ReviewResult for %s, got %+v", tc.Name, result)
				}
				if capture.Calls != saveCalls || capture.Auth != saveAuth {
					t.Fatalf("negative %s: expected calls=%d auth=%d, got calls=%d auth=%d", tc.Name, saveCalls, saveAuth, capture.Calls, capture.Auth)
				}
				negative++
				return
			}
			if tc.WantError {
				t.Fatalf("expected error for %s, got success", tc.Name)
			}
			if result.Verdict != "approve" {
				t.Fatalf("expected approve for %s, got %q", tc.Name, result.Verdict)
			}
			if capture.Calls != saveCalls+1 || capture.Auth != saveAuth+1 {
				t.Fatalf("positive %s: expected calls=%d auth=%d, got calls=%d auth=%d", tc.Name, saveCalls+1, saveAuth+1, capture.Calls, capture.Auth)
			}
			if len(capture.Request.Messages) != 2 {
				t.Fatalf("expected 2 messages, got %d", len(capture.Request.Messages))
			}
			var decoded ReviewTask
			if err := json.Unmarshal([]byte(capture.Request.Messages[1].Content), &decoded); err != nil {
				t.Fatalf("unmarshal message content: %v", err)
			}
			if (decoded.SourceState != "verified_after_change" && decoded.SourceState != "verified_after_change_partial_context") || (len(tc.Task.PartialFiles) > 0 && decoded.SourceState != "verified_after_change_partial_context") || (len(tc.Task.PartialFiles) == 0 && decoded.SourceState != "verified_after_change") {
				t.Fatalf("unexpected source state %q", decoded.SourceState)
			}
			if strings.Contains(decoded.SourceState, "partial") {
				systemMsg := capture.Request.Messages[0].Content
				if !strings.Contains(systemMsg, "not complete files") || !strings.Contains(systemMsg, "hidden source") {
					t.Fatalf("partial positive %s: system message missing required content", tc.Name)
				}
			}
			positive++
		})
	}
	if positive != 9 || negative != 29 {
		t.Fatalf("expected positive=9 negative=29, got positive=%d negative=%d", positive, negative)
	}
	t.Logf("positive=%d negative=%d", positive, negative)
}
