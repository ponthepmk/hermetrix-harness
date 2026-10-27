package worker

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"hermetrix-harness/internal/providers"
)

type fixture struct {
	Source          string          `json:"source"`
	Views           [][]int         `json:"views"`
	Arguments       json.RawMessage `json:"arguments"`
	ExpectedAfter   string          `json:"expected_after"`
	ExpectedRegions []string        `json:"expected_regions"`
}

func TestReviewViewBuilderRegions(t *testing.T) {
	data, err := os.ReadFile("review_region_fixture.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var f fixture
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	views := make([]BoundedFileView, len(f.Views))
	for i, v := range f.Views {
		if len(v) != 2 {
			t.Fatalf("view %d: expected 2 ints", i)
		}
		view, err := NewBoundedFileView("x.go", f.Source, Hash(f.Source), v[0], v[1])
		if err != nil {
			t.Fatalf("NewBoundedFileView %d: %v", i, err)
		}
		views[i] = view
	}
	task := Task{
		ID:            "H1D-B-regions-test-20261026",
		Brief:         "fix authorized text",
		Files:         map[string]string{"x.go": f.Source},
		FileViews:     map[string][]BoundedFileView{"x.go": views},
		WritablePaths: []string{"x.go"},
	}
	fake := &fake{arguments: string(f.Arguments)}
	result, err := Run(context.Background(), fake, providers.Profile{}, "", task)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := ValidateResult(task, result); err != nil {
		t.Fatalf("ValidateResult: %v", err)
	}
	if result.Status != "proposed_unverified" {
		t.Fatalf("status: %q", result.Status)
	}
	if len(result.Changes) != 1 {
		t.Fatalf("changes: %d", len(result.Changes))
	}
	if result.Changes[0].Path != "x.go" || result.Changes[0].Content != f.ExpectedAfter {
		t.Fatalf("change content mismatch")
	}
	whole, files, err := BuildReviewFiles(task, result)
	if err != nil {
		t.Fatalf("BuildReviewFiles: %v", err)
	}
	if len(whole) != 0 {
		t.Fatalf("whole: %v", whole)
	}
	if len(files) != 1 || files["x.go"].Regions == nil {
		t.Fatalf("files: %v", files)
	}
	if len(files["x.go"].Regions) != 3 {
		t.Fatalf("regions: %d", len(files["x.go"].Regions))
	}
	for i, region := range files["x.go"].Regions {
		if region.Before != views[i] || region.AfterContent != f.ExpectedRegions[i] {
			t.Fatalf("region %d mismatch", i)
		}
	}
	if files["x.go"].BeforeSHA256 != Hash(f.Source) {
		t.Fatalf("before sha mismatch")
	}
	if files["x.go"].AfterSHA256 != Hash(f.ExpectedAfter) {
		t.Fatalf("after sha mismatch")
	}
	filesJSON, err := json.Marshal(files)
	if err != nil {
		t.Fatalf("marshal files: %v", err)
	}
	if strings.Contains(string(filesJSON), "HIDDEN_TAIL") {
		t.Fatalf("HIDDEN_TAIL in serialized files")
	}
}
