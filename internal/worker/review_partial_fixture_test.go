package worker

import (
	"context"
	"testing"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/store"
)

type partialReviewCapture struct {
	Calls   int
	Auth    int
	Request providers.ChatRequest
}

func newPartialReviewService(t *testing.T) (*providers.Service, providers.Profile, *partialReviewCapture) {
	ctx := context.Background()
	db, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("db.Close: %v", err)
		}
	})
	capture := &partialReviewCapture{}
	service := providers.NewService(db, reviewAdapter{
		completion: providers.Completion{Content: `{"verdict":"approve","rationale":"fixture","findings":[]}`},
		onRequest: func(req providers.ChatRequest) {
			capture.Calls++
			capture.Request = req
		},
	})
	profile, err := service.Save(ctx, providers.SaveInput{
		Name:            "partial-reviewer",
		BaseURL:         "https://review.example/v1",
		Model:           "review-model",
		ContextWindow:   98304,
		MaxOutputTokens: 4096,
	})
	if err != nil {
		t.Fatalf("service.Save: %v", err)
	}
	return service, profile, capture
}
