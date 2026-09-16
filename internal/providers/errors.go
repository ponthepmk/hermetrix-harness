package providers

import "fmt"

// HTTPError preserves the machine-readable status without requiring callers
// to parse an error string. BodySummary is already bounded and redacted by the
// adapter, but higher-trust boundaries may choose not to expose it at all.
type HTTPError struct {
	Provider    string `json:"provider"`
	StatusCode  int    `json:"status_code"`
	BodySummary string `json:"-"`
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s returned HTTP %d: %s", e.Provider, e.StatusCode, e.BodySummary)
}
