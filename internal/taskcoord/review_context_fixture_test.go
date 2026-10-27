package taskcoord

import (
	"context"
	"strings"
	"testing"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/worker"
)

type reviewProjectionFixture struct {
	service *Service
	profile providers.Profile
	task    worker.Task
	result  worker.Result
	after   map[string]string
	adapter *proposalAdapter
}

func newReviewProjectionFixture(t *testing.T, hidden string, readonly string) reviewProjectionFixture {
	ctx := context.Background()
	storePath := t.TempDir()
	s, err := store.Open(ctx, storePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	adapter := &proposalAdapter{}
	providerService := providers.NewService(s, adapter)

	profile, err := providerService.Save(ctx, providers.SaveInput{
		Name:            "projection-reviewer",
		ContextWindow:   98304,
		MaxOutputTokens: 4096,
		BaseURL:         "https://review.example/v1",
		Model:           "review-model",
	})
	if err != nil {
		t.Fatalf("save profile: %v", err)
	}

	sumSource := "package taskcoord\n\nfunc sum(a, b int) int {\n\treturn a-b\n}\n"
	sumSource += strings.Repeat("// padding\n", 15000)
	sumSource += "// hidden: " + hidden + "\n"
	if len(sumSource) <= 128*1024 {
		t.Fatalf("source too small: %d", len(sumSource))
	}

	start := strings.Index(sumSource, "return a-b")
	if start < 0 {
		t.Fatalf("return a-b not found")
	}
	end := start + len("return a-b")
	view, err := worker.NewBoundedFileView("sum.go", sumSource, worker.Hash(sumSource), start, end)
	if err != nil {
		t.Fatalf("new bounded file view: %v", err)
	}

	task := worker.Task{
		ID:    "projection-fixture",
		Brief: "fix addition",
		Files: map[string]string{
			"sum.go":    sumSource,
			"notes.txt": readonly,
		},
		FileViews: map[string][]worker.BoundedFileView{
			"sum.go": {view},
		},
		WritablePaths: []string{"sum.go"},
	}

	result, err := worker.Run(ctx, adapter, profile, "", task)
	if err != nil {
		t.Fatalf("worker run: %v", err)
	}

	after := map[string]string{}
	for _, change := range result.Changes {
		after[change.Path] = change.Content
	}

	if adapter.calls != 1 {
		t.Fatalf("adapter calls = %d, want 1", adapter.calls)
	}

	return reviewProjectionFixture{
		service: &Service{providers: providerService},
		profile: profile,
		task:    task,
		result:  result,
		after:   after,
		adapter: adapter,
	}
}
