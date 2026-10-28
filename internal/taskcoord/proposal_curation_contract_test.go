package taskcoord

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"

	"hermetrix-harness/internal/inference"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

func TestProposalCurationFrozenCases(t *testing.T) {
	data, err := os.ReadFile("proposal_curation_cases.json")
	if err != nil {
		t.Fatalf("read cases: %v", err)
	}
	var rows []struct {
		Name              string
		Task              worker.Task
		Step              taskengine.Step
		Preset            inference.Preset
		WantError         bool
		ExpectedPartial   []string
		ExpectedCitations []string
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatalf("decode cases: %v", err)
	}
	if len(rows) != 8 {
		t.Fatalf("expected 8 rows, got %d", len(rows))
	}
	// Version 2 wire calibration: measured old14f1b45 worker schema needs 1297 tokens for
	// projected source+small ref; current exclusive-format schema needs 1311 tokens, with
	// the same 2570 body bytes and same source windows. Original fixture max 1299 had a 2
	// token margin. No skip, no other row changes, no weaker expected citations, no production
	// changes.
	for _, row := range rows {
		if row.Name == "optional-refs-fit-from-bounded-wire" {
			curated, err := curateProposalTask(row.Task, row.Step, row.Preset)
			if err != nil {
				t.Fatalf("curateProposalTask: %v", err)
			}
			if len(curated.ProjectBrainRefs) != 0 {
				t.Fatalf("curated.ProjectBrainRefs should be zero, got %d", len(curated.ProjectBrainRefs))
			}
			estimate, err := worker.EstimateProposalInput(curated)
			if err != nil {
				t.Fatalf("worker.EstimateProposalInput: %v", err)
			}
			if estimate.Tokens > 1299 {
				t.Fatalf("estimate.Tokens should be <= 1299, got %d", estimate.Tokens)
			}
			row.Preset.ContextMax = 1313
		}
		before, err := json.Marshal(row.Task)
		if err != nil {
			t.Fatalf("marshal before %s: %v", row.Name, err)
		}
		got, err := curateProposalTask(row.Task, row.Step, row.Preset)
		if err != nil && !row.WantError {
			t.Fatalf("%s: unexpected error: %v", row.Name, err)
		}
		if err == nil && row.WantError {
			t.Fatalf("%s: expected error", row.Name)
		}
		after, err := json.Marshal(row.Task)
		if err != nil {
			t.Fatalf("marshal after %s: %v", row.Name, err)
		}
		if !bytes.Equal(before, after) {
			t.Fatalf("%s: input Task changed", row.Name)
		}
		if row.WantError {
			if !reflect.DeepEqual(got, worker.Task{}) {
				t.Fatalf("%s: error result not zero Task", row.Name)
			}
			continue
		}
		est, err := worker.EstimateProposalInput(got)
		if err != nil {
			t.Fatalf("%s: estimate: %v", row.Name, err)
		}
		if est.Tokens > row.Preset.ContextMax {
			t.Fatalf("%s: tokens %d > %d", row.Name, est.Tokens, row.Preset.ContextMax)
		}
		if est.WireBytes > 128*1024 {
			t.Fatalf("%s: wire bytes %d > 128KiB", row.Name, est.WireBytes)
		}
		actual := make([]string, 0, len(got.FileViews))
		for k := range got.FileViews {
			actual = append(actual, k)
		}
		sort.Strings(actual)
		if len(actual) != len(row.ExpectedPartial) {
			t.Fatalf("%s: partial length %d != %d", row.Name, len(actual), len(row.ExpectedPartial))
		}
		for i, key := range actual {
			if key != row.ExpectedPartial[i] {
				t.Fatalf("%s: partial[%d] %q != %q", row.Name, i, key, row.ExpectedPartial[i])
			}
			for _, view := range got.FileViews[key] {
				if err := worker.ValidateBoundedFileView(key, row.Task.Files[key], worker.Hash(row.Task.Files[key]), view); err != nil {
					t.Fatalf("%s: view %s: %v", row.Name, key, err)
				}
			}
		}
		if len(got.ProjectBrainRefs) != len(row.ExpectedCitations) {
			t.Fatalf("%s: citation count %d != %d", row.Name, len(got.ProjectBrainRefs), len(row.ExpectedCitations))
		}
		for i, ref := range got.ProjectBrainRefs {
			if ref.Citation != row.ExpectedCitations[i] {
				t.Fatalf("%s: citation[%d] %q != %q", row.Name, i, ref, row.ExpectedCitations[i])
			}
		}
		normalized := got
		normalized.FileViews = row.Task.FileViews
		normalized.ProjectBrainRefs = row.Task.ProjectBrainRefs
		norm, err := json.Marshal(normalized)
		if err != nil {
			t.Fatalf("%s: marshal normalized: %v", row.Name, err)
		}
		if !bytes.Equal(before, norm) {
			t.Fatalf("%s: normalized differs from original", row.Name)
		}
		mutated := got
		if len(mutated.Files) > 0 {
			for k := range mutated.Files {
				mutated.Files[k] = "mutated"
				break
			}
		}
		if len(mutated.AcceptanceCriteria) > 0 {
			mutated.AcceptanceCriteria[0] = "mutated"
		}
		mut, err := json.Marshal(row.Task)
		if err != nil {
			t.Fatalf("%s: marshal mutated: %v", row.Name, err)
		}
		if !bytes.Equal(before, mut) {
			t.Fatalf("%s: mutating returned fields changed original marshal", row.Name)
		}
	}
}
