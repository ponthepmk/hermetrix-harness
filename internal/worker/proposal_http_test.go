package worker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"hermetrix-harness/internal/providers"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func TestRealTransportFailureIsOneAttemptAndRedacted(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		if request.URL.String() != "https://provider.invalid/v1/chat/completions" || request.Header.Get("Authorization") != "Bearer bearer-secret" {
			t.Fatalf("unexpected request target or authorization")
		}
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":"private source fragment and bearer-secret"}`)),
			Request:    request,
		}, nil
	})}

	_, err := Run(context.Background(), providers.NewOpenAIAdapter(client), providers.Profile{
		BaseURL: "https://provider.invalid/v1",
		Model:   "fixture",
	}, "bearer-secret", Task{Brief: "fix", Files: map[string]string{"x.go": "package x"}})
	var workerErr *Error
	if !errors.As(err, &workerErr) || workerErr.Code != ErrorProviderFailed {
		t.Fatalf("error = %#v (%v)", workerErr, err)
	}
	if requests.Load() != 1 {
		t.Fatalf("provider requests = %d, want 1", requests.Load())
	}
	if strings.Contains(err.Error(), "private source") || strings.Contains(err.Error(), "bearer-secret") {
		t.Fatalf("provider response escaped boundary: %v", err)
	}
}

func TestProviderHTTPStatusControlsRetryClassification(t *testing.T) {
	for _, test := range []struct {
		status    int
		code      ErrorCode
		retryable bool
	}{
		{status: http.StatusBadRequest, code: ErrorProviderRejected, retryable: false},
		{status: http.StatusUnauthorized, code: ErrorProviderRejected, retryable: false},
		{status: http.StatusRequestTimeout, code: ErrorProviderFailed, retryable: true},
		{status: http.StatusTooManyRequests, code: ErrorProviderFailed, retryable: true},
		{status: http.StatusServiceUnavailable, code: ErrorProviderFailed, retryable: true},
	} {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, Header: http.Header{"Content-Type": []string{"application/json"}},
					Body: io.NopCloser(strings.NewReader(`{"error":"details stay private"}`)), Request: request}, nil
			})}
			_, err := Run(context.Background(), providers.NewOpenAIAdapter(client), providers.Profile{
				BaseURL: "https://provider.invalid/v1", Model: "fixture",
			}, "credential", Task{Brief: "fix", Files: map[string]string{"x.go": "package x"}})
			var workerErr *Error
			if !errors.As(err, &workerErr) || workerErr.Code != test.code || workerErr.Retryable != test.retryable {
				t.Fatalf("status %d classified as %#v (%v)", test.status, workerErr, err)
			}
			if strings.Contains(err.Error(), "details stay private") {
				t.Fatalf("status %d exposed body: %v", test.status, err)
			}
		})
	}
}
