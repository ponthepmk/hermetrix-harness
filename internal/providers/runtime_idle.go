package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ConfirmStoppedRuntime only releases a quarantined local resource after its
// runtime reports every slot idle. HTTP cancellation alone is never proof.
// Unsupported/unreachable runtimes keep their quarantine and the turn lease.
func (s *Service) ConfirmStoppedRuntime(ctx context.Context, profile Profile) error {
	key, local := inferenceResource(profile)
	if !local || !s.scheduler.IsQuarantined(key) {
		return nil
	}
	return s.ConfirmRuntimeIdle(ctx, profile)
}

func (s *Service) ConfirmRuntimeIdle(ctx context.Context, profile Profile) error {
	key, local := inferenceResource(profile)
	if !local {
		return nil
	}
	epoch := s.scheduler.QuarantineEpoch(key)
	credential, err := s.credential(profile)
	if err != nil {
		return err
	}
	endpoint, err := url.Parse(profile.BaseURL)
	if err != nil {
		return err
	}
	endpoint.Path = strings.TrimSuffix(strings.TrimRight(endpoint.Path, "/"), "/v1") + "/slots"
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	client := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		request, err := http.NewRequestWithContext(bounded, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return err
		}
		if credential != "" {
			request.Header.Set("Authorization", "Bearer "+credential)
		}
		response, err := client.Do(request)
		if err == nil {
			var slots []struct {
				ID         *int  `json:"id"`
				Processing *bool `json:"is_processing"`
			}
			limited := &io.LimitedReader{R: response.Body, N: (64 << 10) + 1}
			decoder := json.NewDecoder(limited)
			decodeErr := decoder.Decode(&slots)
			var extra any
			endErr := decoder.Decode(&extra)
			response.Body.Close()
			idle := response.StatusCode == http.StatusOK && decodeErr == nil && errors.Is(endErr, io.EOF) && limited.N > 0 && len(slots) > 0
			ids := map[int]bool{}
			for _, slot := range slots {
				if slot.ID == nil || *slot.ID < 0 || slot.Processing == nil || *slot.Processing {
					idle = false
					break
				}
				if ids[*slot.ID] {
					idle = false
					break
				}
				ids[*slot.ID] = true
			}
			if idle {
				if !s.scheduler.ReleaseQuarantineIfCurrent(key, epoch) {
					return fmt.Errorf("local runtime changed during idle confirmation; inference remains paused")
				}
				return nil
			}
		}
		select {
		case <-bounded.Done():
			return fmt.Errorf("local runtime idle state is unconfirmed; inference remains paused")
		case <-ticker.C:
		}
	}
}
