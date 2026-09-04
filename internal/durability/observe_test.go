package durability

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

type captureHandler struct {
	records chan slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, record slog.Record) error {
	h.records <- record.Clone()
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

func TestObserveExecLogsTheOperationAndError(t *testing.T) {
	handler := &captureHandler{records: make(chan slog.Record, 1)}
	prior := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(prior) })
	Exec("finish test state").Observe(nil, errors.New("disk full"))
	record := <-handler.records
	if record.Message != "durability write failed" {
		t.Fatalf("message = %q", record.Message)
	}
	attributes := map[string]string{}
	record.Attrs(func(attr slog.Attr) bool {
		attributes[attr.Key] = attr.Value.String()
		return true
	})
	if attributes["operation"] != "finish test state" || attributes["error"] != "disk full" {
		t.Fatalf("attributes = %v", attributes)
	}
}
