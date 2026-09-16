package worker

import (
	"context"
	"errors"

	"hermetrix-harness/internal/providers"
)

type ErrorCode string

const (
	ErrorInvalidTask       ErrorCode = "invalid_task"
	ErrorCredentialPresent ErrorCode = "credential_present"
	ErrorProviderTimeout   ErrorCode = "provider_timeout"
	ErrorProviderCancelled ErrorCode = "provider_cancelled"
	ErrorProviderRejected  ErrorCode = "provider_rejected"
	ErrorProviderFailed    ErrorCode = "provider_failed"
	ErrorResponseTruncated ErrorCode = "response_truncated"
	ErrorInvalidProposal   ErrorCode = "invalid_proposal"
)

// Error is deliberately safe to return across the worker boundary. Cause is
// retained for errors.Is inside the process and is never serialized.
type Error struct {
	Code      ErrorCode `json:"code"`
	Phase     Phase     `json:"phase"`
	Retryable bool      `json:"retryable"`
	Message   string    `json:"message"`
	cause     error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.cause }

func workerError(code ErrorCode, phase Phase, retryable bool, message string, cause error) error {
	return &Error{Code: code, Phase: phase, Retryable: retryable, Message: message, cause: cause}
}

func providerError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		return workerError(ErrorProviderTimeout, PhaseRequestingProvider, true, "provider request timed out", context.DeadlineExceeded)
	case errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled):
		return workerError(ErrorProviderCancelled, PhaseRequestingProvider, false, "provider request cancelled", context.Canceled)
	default:
		var httpErr *providers.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode != 408 && httpErr.StatusCode != 429 && httpErr.StatusCode < 500 {
			return workerError(ErrorProviderRejected, PhaseRequestingProvider, false, "provider rejected the request", nil)
		}
		// Provider errors can contain request bodies, response bodies, or URLs.
		// Keep the original error out of both Message and the unwrap chain.
		return workerError(ErrorProviderFailed, PhaseRequestingProvider, true, "provider request failed (details suppressed)", nil)
	}
}
