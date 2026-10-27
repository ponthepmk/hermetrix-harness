package providers

import (
	"context"
	"fmt"
	"hermetrix-harness/internal/inference"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRuntimeIdleRequiresAllValidSlotsBeforeReleasingQuarantine(t *testing.T) {
	for _, test := range []struct {
		name, body string
		idle       bool
	}{
		{"idle", `[{"id":0,"is_processing":false},{"id":1,"is_processing":false}]`, true},
		{"busy", `[{"id":0,"is_processing":false},{"id":1,"is_processing":true}]`, false},
		{"empty", `[]`, false}, {"missing processing", `[{"id":0}]`, false}, {"duplicate", `[{"id":0,"is_processing":false},{"id":0,"is_processing":false}]`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/slots" {
					t.Errorf("unexpected endpoint %s", r.URL.Path)
				}
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			s := NewService(nil, nil)
			profile := Profile{ID: "local", BaseURL: server.URL + "/v1", AdapterKind: AdapterOpenAICompatible}
			key, _ := inferenceResource(profile)
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			// Cancel after dispatch, rather than before admission, to create quarantine.
			active, stop := context.WithCancel(context.Background())
			_, _ = s.scheduler.Do(active, inference.Request{ResourceKey: key, Local: true}, func(context.Context) (inference.Usage, error) { stop(); return inference.Usage{}, context.Canceled })
			if !s.scheduler.IsQuarantined(key) {
				t.Fatal("fixture did not quarantine resource")
			}
			ctx, finish := context.WithTimeout(context.WithoutCancel(cancelled), 120*time.Millisecond)
			defer finish()
			err := s.ConfirmStoppedRuntime(ctx, profile)
			if (err == nil) != test.idle || s.scheduler.IsQuarantined(key) == test.idle {
				t.Fatalf("idle=%v quarantine=%v err=%v", test.idle, s.scheduler.IsQuarantined(key), err)
			}
		})
	}
}
