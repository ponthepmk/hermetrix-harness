package worker

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestReviewRejectsEscapedActiveCredentialBeforeDispatch(t *testing.T) {
	service, profile, capture := newPartialReviewService(t)

	key := "<review-test-secret>"
	t.Setenv("B4_REVIEW_TEST_KEY", key)
	profile.APIKeyEnv = "B4_REVIEW_TEST_KEY"

	if !service.CredentialAppearsIn(profile, key) {
		t.Fatalf("expected active credential to appear in profile")
	}

	var task ReviewTask
	data, err := os.ReadFile("review_input_fixture.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := json.Unmarshal(data, &task); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	task.Objective = key

	body, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal task: %v", err)
	}
	if strings.Contains(string(body), key) {
		t.Fatalf("encoded body contains literal credential")
	}

	runCtx := context.Background()
	result, err := ReviewWithProviderService(runCtx, service, profile, task, Options{
		BeforeProviderRequest: func() error {
			capture.Auth++
			return nil
		},
	})
	if err == nil {
		t.Fatalf("expected nonnil error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "credential") {
		t.Fatalf("error does not mention credential: %v", err)
	}
	if !reflect.DeepEqual(result, ReviewResult{}) {
		t.Fatalf("unexpected result: %+v", result)
	}
	if capture.Calls != 0 || capture.Auth != 0 {
		t.Fatalf("dispatch occurred: calls=%d auth=%d", capture.Calls, capture.Auth)
	}
}
